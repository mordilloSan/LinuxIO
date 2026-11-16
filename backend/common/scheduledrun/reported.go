package scheduledrun

import "errors"

// The exit statuses a worker unit (snapshots, resync, Docker updates) tells
// its end hook the outcome of a failed run with. Any other failure exits 1,
// and the hook raises the unit's run alert.
const (
	// ExitReported: nothing is left to report, since each failure raised its
	// own alert, for a rule, a job, a pool or a container. The hook raises no
	// run alert and clears a standing one.
	ExitReported = 3
	// ExitSkipped: the run did no work, since other work held it off. The
	// hook leaves the run alert as it is. The Docker update pass says so in
	// its summary instead and exits with this only when that was not stored.
	ExitSkipped = 4
)

type reportedError struct{ error }

func (e reportedError) Unwrap() error { return e.error }

// ExitStatus is the exit status of a worker whose run failed with err. A
// worker whose run did no work returns ExitSkipped itself.
func ExitStatus(err error) int {
	if AllReported(err) {
		return ExitReported
	}
	return 1
}

// Unreported is what err holds besides the failures marked Reported: nil when
// every failure in it was reported. Joins are taken apart at every depth, so
// a pass that joins the joined failures of its steps keeps only the
// unreported ones.
func Unreported(err error) error {
	if err == nil || AllReported(err) {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return err
	}
	var rest []error
	for _, e := range joined.Unwrap() {
		if unreported := Unreported(e); unreported != nil {
			rest = append(rest, unreported)
		}
	}
	return errors.Join(rest...)
}

// Reported marks err as a failure its worker raised an alert for. A nil err
// stays nil.
func Reported(err error) error {
	if err == nil {
		return nil
	}
	return reportedError{err}
}

// AllReported reports whether err is a failure and every failure joined in
// it was marked Reported.
func AllReported(err error) bool {
	switch e := err.(type) { //nolint:errorlint // walks the tree itself; errors.As would stop at the first match
	case nil:
		return false
	case reportedError:
		return true
	case interface{ Unwrap() []error }:
		joined := e.Unwrap()
		for _, child := range joined {
			if !AllReported(child) {
				return false
			}
		}
		return len(joined) > 0
	case interface{ Unwrap() error }:
		return AllReported(e.Unwrap())
	}
	return false
}
