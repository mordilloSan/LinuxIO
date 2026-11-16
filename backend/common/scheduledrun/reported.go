package scheduledrun

import "errors"

// ExitSkipped is the exit status of a worker (snapshots, replication,
// resync, Docker updates) whose run did no work, since other work held it
// off, and whose summary, which normally says so, could not be stored. The
// end hook then leaves the run alert as it is. Every other run exits with 0,
// or with 1 for a failure no alert reports (see Conclude).
const ExitSkipped = 4

type reportedError struct{ error }

func (e reportedError) Unwrap() error { return e.error }

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
