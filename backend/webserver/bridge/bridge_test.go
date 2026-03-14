package bridge

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func TestReplaceYamuxSessionReturnsPrevious(t *testing.T) {
	t.Cleanup(resetYamuxSessionsForTest)
	resetYamuxSessionsForTest()

	first := &ipc.YamuxSession{}
	second := &ipc.YamuxSession{}

	if prev := replaceYamuxSession("session-1", first); prev != nil {
		t.Fatalf("first replace returned %v, want nil", prev)
	}
	if prev := replaceYamuxSession("session-1", second); prev != first {
		t.Fatalf("second replace returned %p, want %p", prev, first)
	}
	if got := yamuxSessions.sessions["session-1"]; got != second {
		t.Fatalf("stored session = %p, want %p", got, second)
	}
}

func TestRemoveCurrentYamuxSessionRequiresMatchingPointer(t *testing.T) {
	t.Cleanup(resetYamuxSessionsForTest)
	resetYamuxSessionsForTest()

	old := &ipc.YamuxSession{}
	current := &ipc.YamuxSession{}
	yamuxSessions.sessions["session-1"] = current

	if removed := removeCurrentYamuxSession("session-1", old); removed {
		t.Fatal("removeCurrentYamuxSession removed stale session, want false")
	}
	if got := yamuxSessions.sessions["session-1"]; got != current {
		t.Fatalf("stored session = %p, want %p", got, current)
	}

	if removed := removeCurrentYamuxSession("session-1", current); !removed {
		t.Fatal("removeCurrentYamuxSession did not remove current session")
	}
	if _, ok := yamuxSessions.sessions["session-1"]; ok {
		t.Fatal("session still present after removing current session")
	}
}

func resetYamuxSessionsForTest() {
	yamuxSessions.Lock()
	yamuxSessions.sessions = make(map[string]*ipc.YamuxSession)
	yamuxSessions.Unlock()
}
