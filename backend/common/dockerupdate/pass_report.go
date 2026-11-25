package dockerupdate

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// imageVersionLabel is the OCI label an image names its version with. It is
// read from the image, not from the container, whose labels are its image's
// merged with its own: a container can set the label itself, and one a
// standalone update replaced before it dropped the old image's labels still
// carries that image's version.
const imageVersionLabel = "org.opencontainers.image.version"

// passSkippedBusy is why a pass that found the update lock held did no work.
const passSkippedBusy = "another Docker update was running"

// ContainerOutcome is what a scheduled pass did to one container: it replaced
// it, or failed to, and maybe rolled the failure back, and raised the
// container's alert.
type ContainerOutcome struct {
	Name       string
	Project    string // the Compose project; empty for a standalone container
	Image      string // the image reference the container is created from
	OldImageID string
	NewImageID string // empty for a failed update
	OldVersion string // the image's version label; empty when it has none
	NewVersion string
	Err        error // why the update failed, a rolledBackError when it was rolled back; nil for a replaced container
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
	// LowFreeSpace is a pass that updated nothing because Docker's disk had
	// less free space than the settings ask for (errLowFreeSpace). The run
	// still fails, so its alert reports it.
	LowFreeSpace bool
}

// counts tells the containers the pass replaced, those it failed to, and
// those whose failed update it rolled back.
func (r PassReport) counts() (updated, failed, rolledBack int) {
	for _, outcome := range r.Containers {
		switch {
		case outcome.rolledBack():
			rolledBack++
		case outcome.Err != nil:
			failed++
		default:
			updated++
		}
	}
	return updated, failed, rolledBack
}

// rolledBack reports whether the container's update failed and was rolled
// back to the image it ran before.
func (o ContainerOutcome) rolledBack() bool {
	return errors.As(o.Err, new(rolledBackError))
}

// Summary is the one line the history shows for the pass; ok is false for a
// pass that neither finished its check, reached a container nor found too
// little free space to update.
func (r PassReport) Summary() (summary scheduledrun.Summary, ok bool) {
	if r.Skipped != "" {
		return scheduledrun.Summary{Message: "Skipped: " + r.Skipped, Skipped: true}, true
	}
	if r.Mode == "" && len(r.Containers) == 0 && !r.LowFreeSpace {
		return scheduledrun.Summary{}, false
	}
	updated, failed, rolledBack := r.counts()
	var parts []string
	if r.LowFreeSpace {
		parts = append(parts, "Updates skipped for low free space")
	}
	if updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", updated))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if rolledBack > 0 {
		parts = append(parts, fmt.Sprintf("%d rolled back", rolledBack))
	}
	if r.Checked.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be checked", r.Checked.Errors))
	}
	// What the check found is waiting when the pass touched nothing: all of
	// it in check-only mode, and in update mode the updates of containers it
	// is not set to update. A pass that replaced or failed a container does
	// not say: a failed container may or may not be one the check counted.
	if updated == 0 && failed == 0 && rolledBack == 0 && r.Checked.Updates > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", r.Checked.Updates))
	}
	if len(parts) == 0 {
		return scheduledrun.Summary{Message: "Nothing to update"}, true
	}
	// A rolled back update failed, and raised its alert.
	return scheduledrun.Summary{Message: strings.Join(parts, ", "), Failures: failed + rolledBack}, true
}

// Write prints the pass for the run's output: a line of totals, then one line
// for each container the pass replaced or failed to. The log lines of the
// pass carry the same facts as journal fields, which the saved output of a
// run does not show. It prints nothing where Summary gives no line.
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
		switch {
		case outcome.rolledBack():
			fmt.Fprintf(table, "  rolled back\t%s\t%s\n", name, singleLine(outcome.Err.Error()))
			continue
		case outcome.Err != nil:
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
	return strings.Join(strings.Fields(printableText(text)), " ")
}

// printableText drops, from text others write (an image's labels, the errors
// of a registry, the daemon or Compose), the control characters other than a
// line break or a tab and the invisible format characters, the bidirectional
// controls among them: an escape sequence could change the terminal that
// shows a run's output, and a bidirectional control make a line read as
// another. Bytes that are not UTF-8 become U+FFFD.
func printableText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, "\uFFFD"))
}

// maxVersionRunes bounds the version an image's label names in a report or
// an alert; the image's author writes it.
const maxVersionRunes = 64

// printableVersion is an image's version label on one printable line, cut to
// maxVersionRunes.
func printableVersion(label string) string {
	version := []rune(singleLine(label))
	if len(version) > maxVersionRunes {
		version = append(version[:maxVersionRunes-1], '…')
	}
	return string(version)
}
