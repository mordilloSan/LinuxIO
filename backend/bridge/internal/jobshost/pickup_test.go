package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// pickedUp is what the Resume hook of "test.pickup" saw of a job.
type pickedUp struct {
	id         string
	checkpoint string
}

// addPickUpRoute registers "test.pickup", which resumes in the same boot only,
// after a crash or a stop, as a route whose work runs on outside the job host
// does. Its Resume hook sends what it saw on seen and ends with the outcome
// end returns.
func addPickUpRoute(c *Config, seen chan<- pickedUp, end func(id string) error) {
	addRouteResumingAt(c, "test.pickup", bridgeipc.ResumeInBoot, seen, end)
}

// addRouteResumingAt registers route, resuming at, with addPickUpRoute's hook.
func addRouteResumingAt(c *Config, route string, at bridgeipc.ResumeAt, seen chan<- pickedUp, end func(id string) error) {
	c.Router.Mutation(route,
		func(context.Context, *bridgeipc.Task, any) (any, error) { return nil, nil },
		bridgeipc.WithResume(
			func(context.Context, jsontext.Value) (jsontext.Value, error) { return nil, nil },
			func(_ context.Context, raw jsontext.Value) (any, error) {
				var record jobs.Record
				if err := json.Unmarshal(raw, &record); err != nil {
					return nil, err
				}
				seen <- pickedUp{id: record.ID, checkpoint: string(record.Checkpoint)}
				if err := end(record.ID); err != nil {
					return nil, err
				}
				return map[string]bool{"ok": true}, nil
			}),
		bridgeipc.WithResumeAt(at))
}

func completes(string) error { return nil }

// thisBootRecord is a resumable record an hour old whose host ran in this boot
// and is gone: running, as a crash leaves it, or interrupted by a stop.
func thisBootRecord(t *testing.T, id, route string, state jobs.State) jobs.Record {
	t.Helper()
	boot, err := jobs.BootID()
	if err != nil {
		t.Fatalf("BootID: %v", err)
	}
	r := earlierBootRecord(id, route, state)
	r.Executor.BootID = boot
	return r
}

func receivePickedUp(t *testing.T, ch <-chan pickedUp, n int) map[string]string {
	t.Helper()
	got := map[string]string{}
	for range n {
		select {
		case p := <-ch:
			got[p.id] = p.checkpoint
		case <-time.After(ioTimeout):
			t.Fatalf("picked up %v, want %d jobs", got, n)
		}
	}
	return got
}

// A job of a route that resumes in the boot is picked up at the root host's
// next start after its host crashed or was stopped, with the checkpoint the
// job saved; not after a reboot, not after a cancel, and a route that resumes
// only after a reboot is not picked up after a crash.
func TestRootHostPicksUpWhatItsHostLeftInThisBoot(t *testing.T) {
	seen := make(chan pickedUp, 8)
	resumed := make(chan string, 8)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addPickUpRoute(c, seen, completes)
		addResumableRoute(c, resumed)
	})
	crashed := thisBootRecord(t, jobA, "test.pickup", jobs.StateRunning)
	crashed.Checkpoint = jsontext.Value(`{"transaction":"/7_abc"}`)
	canceled := thisBootRecord(t, jobD, "test.pickup", jobs.StateRunning)
	canceled.CancelRequested = true
	for _, r := range []jobs.Record{
		// Its host crashed: marked crash, then picked up.
		crashed,
		// Its host was stopped by hand in this boot: picked up.
		thisBootRecord(t, jobB, "test.pickup", jobs.StateInterrupted),
		// A reboot interrupted it: the route does not resume after one.
		earlierBootRecord(jobC, "test.pickup", jobs.StateInterrupted),
		// Its owner canceled it before the host crashed.
		canceled,
		// A route that resumes only after a reboot, crashed in this boot.
		thisBootRecord(t, jobE, "test.resumable", jobs.StateRunning),
	} {
		if err := th.cfg.Store.Create(context.Background(), r); err != nil {
			t.Fatalf("Store.Create(%s): %v", r.ID, err)
		}
	}

	th.start()
	// Picked up before the host serves, so a bridge that reconnects finds the
	// job running rather than interrupted.
	if _, ok := th.cfg.Tasks.Get(jobA); !ok {
		t.Fatal("the host served before it picked the job up")
	}
	got := receivePickedUp(t, seen, 2)
	if want := map[string]string{jobA: `{"transaction":"/7_abc"}`, jobB: ""}; !maps.Equal(got, want) {
		t.Fatalf("picked up %v, want %v", got, want)
	}
	waitFor(t, "the picked-up jobs to complete", func() bool {
		return th.record(jobA).State == jobs.StateCompleted && th.record(jobB).State == jobs.StateCompleted
	})
	select {
	case p := <-seen:
		t.Fatalf("also picked up %s", p.id)
	case id := <-resumed:
		t.Fatalf("also resumed %s", id)
	case <-time.After(100 * time.Millisecond):
	}
	for id, reason := range map[string]jobs.Reason{jobC: jobs.ReasonShutdown, jobD: jobs.ReasonCrash, jobE: jobs.ReasonCrash} {
		if r := th.record(id); r.State != jobs.StateInterrupted || r.Reason != reason || r.Resumable {
			t.Fatalf("record %s = %s/%s, resumable %t; want interrupted/%s and no longer resumable", id, r.State, r.Reason, r.Resumable, reason)
		}
	}
	// The crash this start marked alerts only for the jobs it did not pick up.
	var keys []string
	for _, obs := range th.observed() {
		keys = append(keys, obs.Key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{jobD, jobE}) {
		t.Fatalf("alerted %v, want %s and %s", keys, jobD, jobE)
	}
}

// A pick-up whose source of truth knows nothing of the work leaves the job
// interrupted as its host left it, alerted as the interruption would have
// been, with the reason, and a later start in the same boot leaves it alone.
func TestPickUpThatFindsNothingStaysInterrupted(t *testing.T) {
	seen := make(chan pickedUp, 4)
	notFound := func(string) error {
		return fmt.Errorf("%w: PackageKit has no record of transaction /7_abc", bridgeipc.ErrNotResumed)
	}
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addPickUpRoute(c, seen, notFound)
	})
	if err := th.cfg.Store.Create(context.Background(), thisBootRecord(t, jobA, "test.pickup", jobs.StateRunning)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}

	th.start()
	receivePickedUp(t, seen, 1)
	// Raised once the record is back as its host left it.
	waitFor(t, "the interrupted alert", func() bool { return len(th.observed()) == 1 })
	r := th.record(jobA)
	if r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonCrash || r.Resumable {
		t.Fatalf("record = %s/%s, resumable %t; want interrupted/crash and no longer resumable", r.State, r.Reason, r.Resumable)
	}
	if r.Error == nil || !strings.Contains(r.Error.Message, "PackageKit has no record of transaction /7_abc") {
		t.Fatalf("record error = %#v, want the reason the job was not picked up", r.Error)
	}
	obs := th.observed()[0]
	if obs.Key != jobA || obs.Severity != alerts.SeverityError || obs.Title != "test.pickup was interrupted" ||
		!strings.Contains(obs.Message, "(crash)") || !strings.Contains(obs.Message, "PackageKit has no record of transaction /7_abc") {
		t.Fatalf("alert = %#v, want the crash's error alert with the reason", obs)
	}
	if err := th.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	next := newTestHost(t, func(c *Config) {
		c.UID = 0
		c.Store = th.cfg.Store
		addPickUpRoute(c, seen, completes)
	})
	next.start()
	select {
	case p := <-seen:
		t.Fatalf("picked up %s again", p.id)
	case <-time.After(100 * time.Millisecond):
	}
}

// A running job saves where it stands in its record, which is what a pick-up
// after its host died starts from.
func TestRunningJobSavesItsCheckpoint(t *testing.T) {
	saved := make(chan error, 1)
	th := newTestHost(t, nil)
	th.cfg.Router.Mutation("test.checkpoint", func(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
		// A struct, as routes save: a map's keys encode in no set order.
		saved <- bridgeipc.SaveCheckpoint(ctx, struct {
			Index       int    `json:"index"`
			Transaction string `json:"transaction"`
		}{3, "/7_abc"})
		return th.run(ctx, nil, nil)
	})
	th.start()
	th.startJob(th.connect(), "test.checkpoint", jobA)

	select {
	case err := <-saved:
		if err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	case <-time.After(ioTimeout):
		t.Fatal("the job did not save its checkpoint")
	}
	if got := compact(t, th.record(jobA).Checkpoint); got != `{"index":3,"transaction":"/7_abc"}` {
		t.Fatalf("checkpoint = %s, want the one the job saved", got)
	}
	th.release <- nil
}

// A job its host lost without a task to finish it is never picked up: what
// became of it is not known even to the host that had it.
func TestLostJobIsNeverPickedUp(t *testing.T) {
	seen := make(chan pickedUp, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		c.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
		addPickUpRoute(c, seen, completes)
	})
	th.start()
	lost := thisBootRecord(t, jobA, "test.pickup", jobs.StateRunning)
	lost.Executor = th.host.executor
	if err := th.cfg.Store.Create(context.Background(), lost); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	stream := openStream(t, th.connect(), "test.pickup", struct{}{}, jobA)
	if snapshot := readSnapshot(t, stream); snapshot.State != bridgeipc.TaskStateInterrupted {
		t.Fatalf("snapshot state = %s, want interrupted", snapshot.State)
	}
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonCrash || r.Resumable {
		t.Fatalf("record = %s/%s, resumable %t; want interrupted/crash and no longer resumable", r.State, r.Reason, r.Resumable)
	}
}

// A pick-up the host cannot start is alerted once and not tried again at a
// later start in the same boot.
func TestPickUpTheHostCannotStartIsNotRetried(t *testing.T) {
	th := newTestHost(t, func(c *Config) { c.UID = 0 })
	if err := th.cfg.Store.Create(context.Background(), thisBootRecord(t, jobA, "test.pickup", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	seen := make(chan pickedUp, 1)
	// The route is there for the rule, but its request no longer decodes.
	th.cfg.Router.Mutation("test.pickup",
		func(context.Context, *bridgeipc.Task, any) (any, error) { return nil, nil },
		bridgeipc.WithResume(
			func(context.Context, jsontext.Value) (jsontext.Value, error) { return nil, nil },
			func(context.Context, jsontext.Value) (any, error) { seen <- pickedUp{}; return nil, nil }),
		bridgeipc.WithResumeAt(bridgeipc.ResumeInBoot),
		bridgeipc.WithRequestDecoder(func(jsontext.Value) (any, error) { return nil, fmt.Errorf("request changed shape") }))

	th.start()
	waitFor(t, "the failed pick-up alert", func() bool { return len(th.observed()) == 1 })
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Resumable {
		t.Fatalf("record = %s, resumable %t; want interrupted and no longer resumable", r.State, r.Resumable)
	}
	select {
	case <-seen:
		t.Fatal("the Resume hook ran")
	default:
	}
}

// A resumed job that a later shutdown interrupts, through a Suspend that
// stops its Resume hook, is that shutdown's interruption: resumable again with
// its checkpoint, and alerted as one, never as a pick-up that found nothing.
func TestResumedJobInterruptedAgainStaysResumable(t *testing.T) {
	stop := make(chan struct{})
	resumed := make(chan struct{})
	th := newTestHost(t, func(c *Config) { c.UID = 0 })
	th.cfg.Router.Mutation("test.resumable",
		func(context.Context, *bridgeipc.Task, any) (any, error) { return nil, nil },
		bridgeipc.WithResume(
			func(context.Context, jsontext.Value) (jsontext.Value, error) {
				close(stop)
				// The job's own Finish writes its record before the hook returns.
				task, _ := th.cfg.Tasks.Get(jobA)
				<-task.Done()
				return jsontext.Value(`{"at":5}`), nil
			},
			func(context.Context, jsontext.Value) (any, error) {
				close(resumed)
				<-stop
				return nil, errors.New("paused")
			}))
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.resumable", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	select {
	case <-resumed:
	case <-time.After(ioTimeout):
		t.Fatal("the job was not resumed")
	}

	th.cancel() // SIGTERM
	if err := th.wait(); err != nil {
		t.Fatalf("Serve after SIGTERM = %v, want nil", err)
	}
	r := th.record(jobA)
	if r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonShutdown || !r.Resumable || compact(t, r.Checkpoint) != `{"at":5}` {
		t.Fatalf("record = %s/%s, resumable %t, checkpoint %s; want interrupted/shutdown, resumable, {\"at\":5}", r.State, r.Reason, r.Resumable, r.Checkpoint)
	}
	for _, obs := range th.observed() {
		if strings.Contains(obs.Message, "picked up") {
			t.Fatalf("alert = %#v, want a shutdown's", obs)
		}
	}
}

// A crash of a job's resumed run may be the resume's own doing, so it is not
// picked up again: a hook that crashes its host cannot keep the host from
// serving.
func TestCrashOfAPickUpIsNotPickedUpAgain(t *testing.T) {
	seen := make(chan pickedUp, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addPickUpRoute(c, seen, completes)
	})
	crashed := thisBootRecord(t, jobA, "test.pickup", jobs.StateRunning)
	crashed.Resumed = true
	if err := th.cfg.Store.Create(context.Background(), crashed); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}

	th.start()
	select {
	case p := <-seen:
		t.Fatalf("picked up %s again", p.id)
	case <-time.After(100 * time.Millisecond):
	}
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonCrash || r.Resumable {
		t.Fatalf("record = %s/%s, resumable %t; want interrupted/crash and no longer resumable", r.State, r.Reason, r.Resumable)
	}
	if got := th.observed(); len(got) != 1 || got[0].Key != jobA {
		t.Fatalf("alerts = %#v, want the crash's", got)
	}
}

// A job whose record could not be created runs on without one: there is
// nowhere to keep its checkpoint, and no record a pick-up could misread.
func TestCheckpointOfAJobWithoutARecordIsDropped(t *testing.T) {
	th := newTestHost(t, nil)
	if err := th.host.rec.Checkpoint(claim(t, th, jobA), jsontext.Value(`{"index":1}`)); err != nil {
		t.Fatalf("Checkpoint = %v, want nil without a record", err)
	}
}

// A route that resumes in the boot after a crash only (packages.update) is
// not picked up after a stop by hand, which may have been to halt it.
func TestRouteThatResumesAfterACrashIsNotPickedUpAfterAStop(t *testing.T) {
	seen := make(chan pickedUp, 4)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addRouteResumingAt(c, "test.crash_only", bridgeipc.ResumeAfterCrash, seen, completes)
	})
	for _, r := range []jobs.Record{
		thisBootRecord(t, jobA, "test.crash_only", jobs.StateRunning),
		thisBootRecord(t, jobB, "test.crash_only", jobs.StateInterrupted),
	} {
		if err := th.cfg.Store.Create(context.Background(), r); err != nil {
			t.Fatalf("Store.Create(%s): %v", r.ID, err)
		}
	}

	th.start()
	if got := receivePickedUp(t, seen, 1); !maps.Equal(got, map[string]string{jobA: ""}) {
		t.Fatalf("picked up %v, want only the crashed job", got)
	}
	if r := th.record(jobB); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonShutdown || r.Resumable {
		t.Fatalf("stopped job = %s/%s, resumable %t; want interrupted/shutdown and no longer resumable", r.State, r.Reason, r.Resumable)
	}
}

// A route that resumes both in the boot and after a reboot, at a start after
// a reboot, resumes what the shutdown or power loss interrupted and not what
// a crash of the earlier boot did.
func TestRouteResumingBothWaysAfterAReboot(t *testing.T) {
	seen := make(chan pickedUp, 4)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addRouteResumingAt(c, "test.both", bridgeipc.ResumeInBoot|bridgeipc.ResumeAfterReboot, seen, completes)
	})
	crashed := earlierBootRecord(jobB, "test.both", jobs.StateInterrupted)
	crashed.Reason = jobs.ReasonCrash
	for _, r := range []jobs.Record{earlierBootRecord(jobA, "test.both", jobs.StateInterrupted), crashed} {
		if err := th.cfg.Store.Create(context.Background(), r); err != nil {
			t.Fatalf("Store.Create(%s): %v", r.ID, err)
		}
	}

	th.start()
	if got := receivePickedUp(t, seen, 1); !maps.Equal(got, map[string]string{jobA: ""}) {
		t.Fatalf("resumed %v, want only the job the shutdown interrupted", got)
	}
	if r := th.record(jobB); r.Resumable {
		t.Fatal("a crash of an earlier boot stayed resumable")
	}
}

// A stop by hand of a resumed run is picked up again: only a crash may be the
// resume's own doing.
func TestStopOfAPickUpIsPickedUpAgain(t *testing.T) {
	seen := make(chan pickedUp, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addPickUpRoute(c, seen, completes)
	})
	stopped := thisBootRecord(t, jobA, "test.pickup", jobs.StateInterrupted)
	stopped.Resumed = true
	if err := th.cfg.Store.Create(context.Background(), stopped); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	receivePickedUp(t, seen, 1)
}

// A pick-up after a stop by hand that finds nothing keeps the stop's reason
// and its warning, with why.
func TestPickUpAfterAStopThatFindsNothingKeepsItsWarning(t *testing.T) {
	seen := make(chan pickedUp, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addPickUpRoute(c, seen, func(string) error { return fmt.Errorf("%w: the drive has no result", bridgeipc.ErrNotResumed) })
	})
	if err := th.cfg.Store.Create(context.Background(), thisBootRecord(t, jobA, "test.pickup", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	waitFor(t, "the interrupted alert", func() bool { return len(th.observed()) == 1 })
	if r := th.record(jobA); r.Reason != jobs.ReasonShutdown || r.Resumable {
		t.Fatalf("record = %s/%s, resumable %t; want interrupted/shutdown and no longer resumable", r.State, r.Reason, r.Resumable)
	}
	if obs := th.observed()[0]; obs.Severity != alerts.SeverityWarning || !strings.Contains(obs.Message, "(shutdown)") || !strings.Contains(obs.Message, "the drive has no result") {
		t.Fatalf("alert = %#v, want the stop's warning with why", obs)
	}
}

// A Resume hook that finds nothing while the host is stopping it is that
// stop's interruption: resumable again, judged again at the next start.
func TestNotResumedDuringAStopIsTheStops(t *testing.T) {
	verdict := make(chan struct{})
	resumed := make(chan struct{})
	th := newTestHost(t, func(c *Config) { c.UID = 0 })
	th.cfg.Router.Mutation("test.pickup",
		func(context.Context, *bridgeipc.Task, any) (any, error) { return nil, nil },
		bridgeipc.WithResume(
			func(context.Context, jsontext.Value) (jsontext.Value, error) {
				close(verdict)
				task, _ := th.cfg.Tasks.Get(jobA)
				<-task.Done()
				return nil, nil
			},
			func(context.Context, jsontext.Value) (any, error) {
				close(resumed)
				<-verdict
				return nil, fmt.Errorf("%w: no transaction", bridgeipc.ErrNotResumed)
			}),
		bridgeipc.WithResumeAt(bridgeipc.ResumeInBoot))
	if err := th.cfg.Store.Create(context.Background(), thisBootRecord(t, jobA, "test.pickup", jobs.StateRunning)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	select {
	case <-resumed:
	case <-time.After(ioTimeout):
		t.Fatal("the job was not picked up")
	}

	th.cancel() // SIGTERM
	if err := th.wait(); err != nil {
		t.Fatalf("Serve after SIGTERM = %v, want nil", err)
	}
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonShutdown || !r.Resumable {
		t.Fatalf("record = %s/%s, resumable %t; want interrupted/shutdown and resumable", r.State, r.Reason, r.Resumable)
	}
}

// A resume of an earlier boot's job waits for the host to serve: building the
// job owner's handlers can hang, as on a home folder on a network mount, and
// must not keep the host from serving. A same-boot pick-up runs before.
func TestResumeAfterARebootWaitsForTheHostToServe(t *testing.T) {
	release := make(chan struct{})
	resumed := make(chan string, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addResumableRoute(c, resumed)
		router := c.Router
		c.Handlers = func(*session.Session) (*bridgeipc.Router, error) {
			<-release
			return router, nil
		}
	})
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.resumable", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})

	th.start() // READY while the owner's handlers are still being built
	released = true
	close(release)
	if got := receiveIDs(t, resumed, 1); !slices.Equal(got, []string{jobA}) {
		t.Fatalf("resumed %v, want %s", got, jobA)
	}
}

// A host told to stop as it starts leaves the jobs it was to resume as they
// were, for its next start to resume under the same rule, and alerts nothing.
func TestResumeCutByAStopLeavesTheJobResumable(t *testing.T) {
	resumed := make(chan string, 1)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addResumableRoute(c, resumed)
	})
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.resumable", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	th.host.resumeInterrupted(ctx, []string{jobA})

	if r := th.record(jobA); r.State != jobs.StateInterrupted || !r.Resumable || r.Executor.BootID != "an-earlier-boot" {
		t.Fatalf("record = %s, resumable %t, boot %q; want it as it was", r.State, r.Resumable, r.Executor.BootID)
	}
	if got := th.observed(); len(got) != 0 {
		t.Fatalf("alerts = %#v, want none", got)
	}
}

// A resume after a reboot that the host cannot start is stamped with this
// boot, as before same-boot pick-ups existed, rather than made unresumable:
// the next start of this boot is what lets it go.
func TestFailedResumeAfterARebootIsStamped(t *testing.T) {
	th := newTestHost(t, func(c *Config) { c.UID = 0 })
	// Its route is gone after an update, so the resume fails.
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.gone", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	waitFor(t, "the failed resume alert", func() bool { return len(th.observed()) == 1 })
	if r := th.record(jobA); r.State != jobs.StateInterrupted || !r.Resumable || r.Executor.BootID != th.host.executor.BootID {
		t.Fatalf("record = %s, resumable %t, boot %q; want interrupted, resumable and stamped with this boot", r.State, r.Resumable, r.Executor.BootID)
	}
}
