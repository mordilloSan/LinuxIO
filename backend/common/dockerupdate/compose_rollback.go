package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	// composeRollbackTimeout bounds a Compose rollback, which runs detached
	// from the request so that a cancel of the request cannot stop it
	// half-way: `up` from the image on the host and the readiness wait of
	// the restored containers.
	composeRollbackTimeout = 2*containerReadyTimeout + containerSettleTime
	// composeTagTimeout bounds each read or move of the tag, on a context of
	// its own: the tag goes back on the new image after a failed `up` even
	// when the rollback's own time ran out during it.
	composeTagTimeout = 30 * time.Second
)

// errNotRolledBack marks a failed Compose update that a rollback could not
// undo, and so left as it is.
var errNotRolledBack = errors.New("not rolled back")

// errStillComingUp is why a new container that did not fail is not rolled
// back: one still coming up when the wait ended (health starting, or running
// and not yet settled) may have begun to change its data, such as a
// migration at start, and the old image must not be started on it.
var errStillComingUp = fmt.Errorf("%w: the new container had not exited, turned unhealthy or kept restarting when the wait ended", errNotRolledBack)

// errNoVerdict is why a new container whose wait ended without a verdict on
// it (paused, a state that cannot be read, not found) is not rolled back:
// how it runs is unknown.
var errNoVerdict = fmt.Errorf("%w: the wait gave no verdict on the new container", errNotRolledBack)

// rollbackRefusal is why a Compose service whose new container failed its
// verify with err is not rolled back; nil when the container itself failed
// (containerFailedError), which a rollback answers.
func rollbackRefusal(err error) error {
	switch {
	case errors.As(err, new(containerFailedError)):
		return nil
	case errors.As(err, new(notReadyInTimeError)):
		return errStillComingUp
	default:
		return errNoVerdict
	}
}

// composeServiceSettings is what a rollback needs of a service's effective
// configuration.
type composeServiceSettings struct {
	Image      string
	PullPolicy string
	Build      bool // the service has a build section
}

type composeRollbackClient interface {
	containerReadinessClient
	imageInspector
	ImageTag(context.Context, client.ImageTagOptions) (client.ImageTagResult, error)
}

// composeRollback puts Compose services whose update failed back on the
// image they ran before. Tests replace its commands.
type composeRollback struct {
	cli composeRollbackClient
	// settings reads services of a project as Compose resolves them.
	settings func(context.Context, ComposeTarget, []string) (map[string]composeServiceSettings, error)
	// restore brings services up on the images the host holds.
	restore func(context.Context, ComposeTarget, []string, ComposeLineEmitter) error
}

func newComposeRollback(cli composeRollbackClient) composeRollback {
	return composeRollback{cli: cli, settings: composeServiceSettingsOf, restore: composeUpFromLocalImages}
}

// composeRollbackService is a service a rollback brings up again, and the
// name of its container.
type composeRollbackService struct {
	Service   string
	Container string
}

// normalizedReference is ref as Docker names its tag, so two spellings of
// one tag (nginx, docker.io/library/nginx:latest) are one; ref itself when it
// cannot be parsed.
func normalizedReference(ref string) string {
	normalized, _, _, err := NormalizeImageReference(ref)
	if err != nil {
		return ref
	}
	return normalized
}

// taggedImage is the ID of the image the tag imageRef names now, the one
// the update pulled. It is read before any rollback moves the tag.
func (r composeRollback) taggedImage(ctx context.Context, imageRef string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), composeTagTimeout)
	defer cancel()
	tagged, err := r.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", tagCallError(ctx, fmt.Sprintf("inspect image %q", imageRef), err)
	}
	return tagged.ID, nil
}

// tagCallError is the error of a read or move of the tag on ctx, its own
// context: its own text when that ran out of time, since a wrapped deadline
// error reads as "operation timed out" alone in the bridge's task runner.
func tagCallError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: no answer within %s", operation, composeTagTimeout)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// run puts the tag imageRef (normalised) back on oldImageID, the image the
// services ran before the update, read before the pull, and brings services
// up on it again, waiting for each to be ready and checking that it runs
// oldImageID. pulledImageID is what the tag named before the rollback
// (taggedImage). run is detached from ctx's cancellation and bounded by
// composeRollbackTimeout. changed is false when nothing on the host was
// changed: the rollback was not possible (errNotRolledBack) or failed before
// the tag moved. When `up` fails, the tag is put back on pulledImageID; both
// errors are returned.
func (r composeRollback) run(ctx context.Context, target ComposeTarget, imageRef, oldImageID, pulledImageID string, services []composeRollbackService, dependents []string) (changed bool, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), composeRollbackTimeout)
	defer cancel()
	if checkErr := r.check(ctx, target, imageRef, services); checkErr != nil {
		return false, checkErr
	}
	if pulledImageID == oldImageID {
		return false, fmt.Errorf("%w: the tag %q still names the image the service ran before", errNotRolledBack, imageRef)
	}
	if tagErr := r.tag(ctx, oldImageID, imageRef); tagErr != nil {
		return false, tagErr
	}

	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, service.Service)
	}
	// The services that join one of them lose its namespace again.
	names = append(names, dependents...)
	collector := &ComposeMessageCollector{}
	if restoreErr := r.restore(ctx, target, names, collector.Emit); restoreErr != nil {
		upErr := fmt.Errorf("bring up Compose services %q on image %s: %w", names, shortImageID(oldImageID), restoreErr)
		if output := collector.String(); output != "" {
			upErr = fmt.Errorf("%w: %s", upErr, output)
		}
		return true, errors.Join(upErr, r.tag(ctx, pulledImageID, imageRef))
	}
	var errs []error
	for _, service := range services {
		restored, waitErr := inspectAndWaitForComposeContainer(ctx, r.cli, service.Container)
		switch {
		case waitErr != nil && ctx.Err() != nil:
			// Not wrapped: a deadline error reads as "operation timed out"
			// alone in the bridge's task runner.
			errs = append(errs, fmt.Errorf("restored container %q was not ready when the rollback's limit of %s ended", service.Container, composeRollbackTimeout))
			continue
		case waitErr != nil:
			errs = append(errs, fmt.Errorf("restored container %q: %w", service.Container, waitErr))
			continue
		case restored.Image != oldImageID:
			errs = append(errs, fmt.Errorf("restored container %q runs image %s, not %s", service.Container, shortImageID(restored.Image), shortImageID(oldImageID)))
			continue
		}
		slog.Info("rolled back Compose Docker container "+service.Container,
			"component", "docker",
			"subsystem", "update",
			"project", target.Name,
			"container", service.Container,
			"image", imageRef,
			"old_image", oldImageID,
			"new_image", pulledImageID)
	}
	return true, errors.Join(errs...)
}

// check refuses a rollback the tag cannot carry: an image pinned by digest
// has no tag to move, a service pulled at every `up` (pull_policy: always)
// would not stay on the old image, and a service whose files now name another
// image would not be brought up on this one. A service with a build section
// is rolled back like one that is pulled: the `up` of a rollback builds
// nothing. One that names no image runs Compose's default name for it, the
// tag its container was created from.
func (r composeRollback) check(ctx context.Context, target ComposeTarget, imageRef string, services []composeRollbackService) error {
	_, _, pinned, err := NormalizeImageReference(imageRef)
	if err != nil {
		return err
	}
	if pinned {
		return fmt.Errorf("%w: image %q is pinned by digest", errNotRolledBack, imageRef)
	}
	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, service.Service)
	}
	settings, err := r.settings(ctx, target, names)
	if err != nil {
		return err
	}
	for _, service := range services {
		setting, ok := settings[service.Service]
		switch {
		case !ok:
			return fmt.Errorf("%w: the project no longer has service %q", errNotRolledBack, service.Service)
		case strings.EqualFold(setting.PullPolicy, "always"):
			return fmt.Errorf("%w: service %q has pull_policy: always", errNotRolledBack, service.Service)
		case setting.Image != "" && normalizedReference(setting.Image) != imageRef:
			return fmt.Errorf("%w: service %q now names image %q", errNotRolledBack, service.Service, setting.Image)
		}
	}
	return nil
}

// tag points imageRef at imageID, on a context of its own (composeTagTimeout).
func (r composeRollback) tag(ctx context.Context, imageID, imageRef string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), composeTagTimeout)
	defer cancel()
	if _, err := r.cli.ImageTag(ctx, client.ImageTagOptions{Source: imageID, Target: imageRef}); err != nil {
		return tagCallError(ctx, fmt.Sprintf("tag image %s as %q", shortImageID(imageID), imageRef), err)
	}
	return nil
}

// rolledBackError is the failure of an update that was undone: the container
// runs again what it ran before, which to names.
type rolledBackError struct {
	err error
	to  string
}

func (e rolledBackError) Error() string {
	return e.err.Error() + "; rolled back to " + e.to
}

func (e rolledBackError) Unwrap() error { return e.err }

// composeUpdateFailure is the error of a Compose update whose new container
// failed the readiness wait with cause, once its rollback to oldImageID has
// run: cause marked rolled back, cause with why it was not rolled back, or
// cause with the rollback's own failure.
func composeUpdateFailure(cause error, oldImageID string, rollbackErr error) error {
	switch {
	case rollbackErr == nil:
		return rolledBackError{err: cause, to: "image " + shortImageID(oldImageID)}
	case errors.Is(rollbackErr, errNotRolledBack):
		return fmt.Errorf("%w; %w", cause, rollbackErr)
	default:
		return fmt.Errorf("%w; rollback failed: %w", cause, rollbackErr)
	}
}

// configImage is the image reference a container was created from.
func configImage(inspect container.InspectResponse) string {
	if inspect.Config == nil {
		return ""
	}
	return inspect.Config.Image
}
