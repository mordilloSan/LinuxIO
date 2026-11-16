package dockerupdate

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// imageVersionLabel is the OCI label an image names its version with. It is
// read from the image, not from the container: a standalone replacement is
// created from a copy of the old container's configuration, labels included,
// so its labels still name the old image's version.
const imageVersionLabel = "org.opencontainers.image.version"

// passSkippedBusy is why a pass that found the update lock held did no work.
const passSkippedBusy = "another Docker update was running"

// ContainerOutcome is what a scheduled pass did to one container: it replaced
// it, or failed to and raised the container's alert.
type ContainerOutcome struct {
	Name       string
	Project    string // the Compose project; empty for a standalone container
	Image      string // the image reference the container is created from
	OldImageID string
	NewImageID string // empty for a failed update
	OldVersion string // the image's version label; empty when it has none
	NewVersion string
	Err        error // why the update failed; nil for a replaced container
}

// outcomeBefore starts the outcome of a container from its inspection before
// the update.
func outcomeBefore(before container.InspectResponse) ContainerOutcome {
	outcome := ContainerOutcome{
		Name:       strings.TrimPrefix(before.Name, "/"),
		OldImageID: before.Image,
	}
	if before.Config != nil {
		outcome.Image = before.Config.Image
		outcome.Project = strings.TrimSpace(before.Config.Labels["com.docker.compose.project"])
	}
	return outcome
}

// change says what replaced what, with the versions when both images name
// one: "nginx:1.27 1.27.2 -> 1.27.3 (1a2b3c4d5e6f -> 4d5e6f708192)".
func (o ContainerOutcome) change() string {
	ids := fmt.Sprintf("(%s -> %s)", shortImageID(o.OldImageID), shortImageID(o.NewImageID))
	parts := make([]string, 0, 3)
	if o.Image != "" {
		parts = append(parts, o.Image)
	}
	if o.OldVersion != "" && o.NewVersion != "" && o.OldVersion != o.NewVersion {
		parts = append(parts, o.OldVersion+" -> "+o.NewVersion)
	}
	return strings.Join(append(parts, ids), " ")
}

// sentence is the change as an alert says it.
func (o ContainerOutcome) sentence() string {
	oldID, newID := shortImageID(o.OldImageID), shortImageID(o.NewImageID)
	switch {
	case o.Image == "":
		return "Image " + oldID + " was replaced by " + newID + "."
	case o.OldVersion != "" && o.NewVersion != "" && o.OldVersion != o.NewVersion:
		return fmt.Sprintf("Image %s went from version %s to %s (%s to %s).", o.Image, o.OldVersion, o.NewVersion, oldID, newID)
	}
	return fmt.Sprintf("Image %s was replaced (%s to %s).", o.Image, oldID, newID)
}

// PassReport is what one scheduled pass did, for the run's printed report and
// the summary kept in its record. The zero report is that of a pass that
// ended before it checked anything.
type PassReport struct {
	// Skipped is why the pass did no work; empty for a pass that ran.
	Skipped string
	// Mode is the configured mode, "update" or "check_only", once the check
	// of the pass has succeeded; empty for a pass whose check failed.
	Mode       DockerContainerAutoUpdateMode
	Checked    DockerUpdateCheckResult
	Containers []ContainerOutcome
}

func (r PassReport) counts() (updated, failed int) {
	for _, outcome := range r.Containers {
		if outcome.Err != nil {
			failed++
		} else {
			updated++
		}
	}
	return updated, failed
}

// Summary is the one line the history shows for the pass; ok is false for a
// pass that neither finished its check nor reached a container.
func (r PassReport) Summary() (summary scheduledrun.Summary, ok bool) {
	if r.Skipped != "" {
		return scheduledrun.Summary{Message: "Skipped: " + r.Skipped, Skipped: true}, true
	}
	if r.Mode == "" && len(r.Containers) == 0 {
		return scheduledrun.Summary{}, false
	}
	updated, failed := r.counts()
	var parts []string
	if updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", updated))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if r.Checked.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be checked", r.Checked.Errors))
	}
	// What the check found is waiting when the pass touched nothing: all of
	// it in check-only mode, and in update mode the updates of containers it
	// is not set to update. A pass that replaced or failed a container does
	// not say: a failed container may or may not be one the check counted.
	if updated == 0 && failed == 0 && r.Checked.Updates > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", r.Checked.Updates))
	}
	if len(parts) == 0 {
		return scheduledrun.Summary{Message: "Nothing to update"}, true
	}
	return scheduledrun.Summary{Message: strings.Join(parts, ", "), Failures: failed}, true
}

// Write prints the pass for the run's output: a line of totals, then one line
// for each container the pass replaced or failed to. The log lines of the
// pass carry the same facts as journal fields, which the saved output of a
// run does not show. It prints nothing for a pass that neither finished its
// check nor reached a container.
func (r PassReport) Write(w io.Writer) {
	summary, ok := r.Summary()
	if !ok {
		return
	}
	if r.Skipped != "" {
		fmt.Fprintf(w, "Container updates: skipped, %s\n", r.Skipped)
		return
	}
	if r.Mode == "" {
		// The check failed, so there is no count to give.
		fmt.Fprintf(w, "Container updates: %s\n", lowerFirst(summary.Message))
	} else {
		fmt.Fprintf(w, "Container updates: %d checked, %s\n", r.Checked.Checked, lowerFirst(summary.Message))
	}
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, outcome := range r.Containers {
		name := outcome.Name
		if outcome.Project != "" {
			name += " (" + outcome.Project + ")"
		}
		if outcome.Err != nil {
			fmt.Fprintf(table, "  failed\t%s\t%s\n", name, singleLine(outcome.Err.Error()))
			continue
		}
		fmt.Fprintf(table, "  updated\t%s\t%s\n", name, outcome.change())
	}
	_ = table.Flush()
}

func lowerFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToLower(text[:1]) + text[1:]
}

// singleLine keeps an error that carries Compose output on one journal line.
func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
