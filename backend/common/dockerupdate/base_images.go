package dockerupdate

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	digest "github.com/opencontainers/go-digest"
)

// baseImage is a base image a build resolved: the tagged reference it
// follows and the registry digest the tag named then.
type baseImage struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

// builtImageRecord is what LinuxIO's rebuild recorded about the image it
// built: the base images the build resolved, which a check compares with
// their registries. Bases is empty for an image built FROM scratch or on
// bases pinned by digest alone, and when BasesUnread is set.
type builtImageRecord struct {
	ImageID string      `json:"image_id"`
	Bases   []baseImage `json:"bases"`
	// BasesUnread is a build whose output looked a tag up and named no base
	// it resolved: the output was not understood, so no base is watched.
	BasesUnread bool      `json:"bases_unread,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`
}

// maxBuiltImageBases is how many base images one record keeps.
const maxBuiltImageBases = 32

var (
	// buildBaseLine is the line BuildKit's plain progress writes for a base
	// image the build resolved, "#4 [app 1/2] FROM
	// docker.io/library/alpine:3.20@sha256:…" from Compose's own builder and
	// "#5 [1/2] FROM …" through buildx and Bake: the bracket holds the Compose
	// service with Compose's builder, then the stage when it has a name, then
	// the step. The output of a step is written "#<n> <seconds> <text>", so no
	// step can write a line that matches. A stage built FROM an earlier stage,
	// and FROM scratch, have no such line.
	buildBaseLine = regexp.MustCompile(`^#[0-9]+ \[(?:[^\]\s]+ )*[0-9]+/[0-9]+\] FROM (\S+)$`)
	// buildMetadataLine is the line for a reference the build looked up,
	// "#2 [app internal] load metadata for docker.io/library/alpine:3.20",
	// "[internal]" through Bake. A base the Dockerfile pins by digest is
	// looked up with its digest, while its FROM line reads as that of a tag
	// the build resolved.
	buildMetadataLine = regexp.MustCompile(`^#[0-9]+ \[(?:[^\]\s]+ )?internal\] load metadata for (\S+)$`)
)

// buildBaseCollector reads the base images a build resolved from the lines of
// its plain progress output (add), never from the Dockerfile or anything else
// in the build context.
type buildBaseCollector struct {
	resolved []resolvedBase
	pinned   map[string]bool // the references looked up by digest
	// taggedLookup is a tag the build looked up: it resolved a base from it.
	taggedLookup bool
}

type resolvedBase struct {
	canonical string // reference, tag and digest, as a metadata line names a pinned one
	base      baseImage
}

// add reads one line of the build's output.
func (c *buildBaseCollector) add(line string) {
	if match := buildMetadataLine.FindStringSubmatch(line); match != nil {
		if named, err := reference.ParseNormalizedNamed(match[1]); err == nil {
			if _, digested := named.(reference.Digested); !digested {
				c.taggedLookup = true
				return
			}
			if c.pinned == nil {
				c.pinned = make(map[string]bool)
			}
			c.pinned[named.String()] = true
		}
		return
	}
	match := buildBaseLine.FindStringSubmatch(line)
	if match == nil || len(c.resolved) == maxBuiltImageBases {
		return
	}
	resolved, ok := parseResolvedBase(match[1])
	if !ok || slices.Contains(c.resolved, resolved) {
		return
	}
	c.resolved = append(c.resolved, resolved)
}

// record is what the build's output says of imageID, the image it gave: each
// base image it resolved from a tag, in the order it wrote them, a base the
// Dockerfile pins by digest left out since it never moves. A build that looked
// a tag up and named no base it resolved was not understood: its record says
// so, rather than that the image has no base that can move.
func (c *buildBaseCollector) record(imageID string, recordedAt time.Time) builtImageRecord {
	record := builtImageRecord{ImageID: imageID, Bases: []baseImage{}, RecordedAt: recordedAt}
	if c.taggedLookup && len(c.resolved) == 0 {
		record.BasesUnread = true
		return record
	}
	for _, resolved := range c.resolved {
		if !c.pinned[resolved.canonical] {
			record.Bases = append(record.Bases, resolved.base)
		}
	}
	return record
}

// parseResolvedBase reads the reference of a FROM line, which BuildKit writes
// with the tag it resolved and the digest it got for it. A reference with no
// tag is pinned by digest alone, and is not a base that can move.
func parseResolvedBase(value string) (resolvedBase, bool) {
	named, err := reference.ParseNormalizedNamed(value)
	if err != nil {
		return resolvedBase{}, false
	}
	tagged, isTagged := named.(reference.NamedTagged)
	digested, isDigested := named.(reference.Digested)
	if !isTagged || !isDigested || digested.Digest().Validate() != nil {
		return resolvedBase{}, false
	}
	ref, err := reference.WithTag(reference.TrimNamed(named), tagged.Tag())
	if err != nil {
		return resolvedBase{}, false
	}
	return resolvedBase{
		canonical: named.String(),
		base:      baseImage{Ref: ref.String(), Digest: digested.Digest().String()},
	}, true
}

// builtCheck is what a check found for a container whose image its Compose
// service builds on this host: why it is built, and whether a base image the
// last rebuild by LinuxIO resolved has a newer version on its registry.
type builtCheck struct {
	reason    string
	baseMoved bool
	// remoteDigest stands for the digests the moved base images have now,
	// so that a base that moves again is a new occurrence of the alert.
	remoteDigest string
}

const (
	builtWithoutRecord = "; rebuild it once from LinuxIO to be told when its base images have newer versions"
	builtBasesUnread   = "; its base images could not be read from the build's output, so they are not watched"
	builtWithoutBases  = "; it has no base image that can have a newer version (FROM scratch, or pinned by digest)"
	builtBasesCurrent  = "; its base images have no newer version"
)

// rebuiltCheck is what a check finds right after a rebuild recorded record:
// the build pulled every base image, so none has a newer version.
func rebuiltCheck(reason string, record builtImageRecord) builtCheck {
	switch {
	case record.BasesUnread:
		return builtCheck{reason: reason + builtBasesUnread}
	case len(record.Bases) == 0:
		return builtCheck{reason: reason + builtWithoutBases}
	}
	return builtCheck{reason: reason + builtBasesCurrent}
}

// recordedBaseRefs is each base image reference recorded for the image of one
// of targets, once. Records are kept by image ID, so the check knows them
// before it asks any registry, and asks for them with its images.
func recordedBaseRefs(records map[string]builtImageRecord, targets []containerImageUpdateTarget) []string {
	var refs []string
	for _, target := range targets {
		for _, base := range records[target.ImageID].Bases {
			if !slices.Contains(refs, base.Ref) {
				refs = append(refs, base.Ref)
			}
		}
	}
	return refs
}

// checkBuiltBases compares the base images recorded for the image of each of
// targets that built names (its builtReason, by container ID) with what their
// registries name now, each reference asked once, as the check asks for an
// image. It returns an error only when ctx ends.
func checkBuiltBases(ctx context.Context, cli imageUpdateCheckClient, targets []containerImageUpdateTarget, built map[string]string) (map[string]builtCheck, error) {
	if len(built) == 0 {
		return nil, nil
	}
	records := readBuiltImageRecords()
	current, err := inspectImageUpdateRequests(ctx, cli, nil, recordedBaseRefs(records, targets))
	if err != nil {
		return nil, err
	}
	return compareBuiltContainers(targets, built, records, current), nil
}

// baseLookup is what the registry named for a base image's reference: its
// digest now, or why it could not be asked.
type baseLookup struct {
	digest digest.Digest
	err    error
}

// lookupBaseDigest asks the registry for the digest ref names now
// (registryDigest). A lookup that fails is kept with its error; only the end
// of ctx is returned.
func lookupBaseDigest(ctx context.Context, cli imageUpdateCheckClient, ref string) (baseLookup, error) {
	found, err := registryDigest(ctx, cli, ref)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return baseLookup{}, ctxErr
	}
	if err == nil {
		err = found.Validate()
	}
	return baseLookup{digest: found, err: err}, nil
}

// compareBuiltContainers is the check of each of targets that built names
// (its builtReason, by container ID), from the records of their images and
// the digests their base images' registries name now (current).
func compareBuiltContainers(targets []containerImageUpdateTarget, built map[string]string, records map[string]builtImageRecord, current map[string]baseLookup) map[string]builtCheck {
	checks := make(map[string]builtCheck, len(built))
	for _, target := range targets {
		reason, ok := built[target.ContainerID]
		if !ok {
			continue
		}
		record, recorded := records[target.ImageID]
		checks[target.ContainerID] = compareBuiltBases(reason, record, recorded, current)
	}
	return checks
}

// compareBuiltBases is the check of a built container, built for reason,
// whose image has record when recorded, against the digests its base images'
// registries name now (current). A base that moved makes it available; one
// that could not be asked is named in the reason, and is no error.
func compareBuiltBases(reason string, record builtImageRecord, recorded bool, current map[string]baseLookup) builtCheck {
	if !recorded {
		return builtCheck{reason: reason + builtWithoutRecord}
	}
	var moved, now, unchecked []string
	for _, base := range record.Bases {
		found, asked := current[base.Ref]
		switch {
		case !asked:
			unchecked = append(unchecked, "base image "+base.Ref+" was not checked")
		case found.err != nil:
			unchecked = append(unchecked, fmt.Sprintf("base image %s could not be checked: %s", base.Ref, singleLine(found.err.Error())))
		case found.digest.String() != base.Digest:
			moved = append(moved, base.Ref)
			now = append(now, base.Ref+"@"+found.digest.String())
		}
	}
	if len(moved) == 0 {
		if len(unchecked) > 0 {
			return builtCheck{reason: reason + "; " + strings.Join(unchecked, "; ")}
		}
		return rebuiltCheck(reason, record)
	}
	text := "base image " + moved[0] + " has a newer version on its registry"
	if len(moved) > 1 {
		text = "base images " + strings.Join(moved, ", ") + " have newer versions on their registries"
	}
	parts := append([]string{text, reason}, unchecked...)
	return builtCheck{
		reason:       strings.Join(parts, "; "),
		baseMoved:    true,
		remoteDigest: digest.FromString(strings.Join(now, "\n")).String(),
	}
}
