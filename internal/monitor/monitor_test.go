package monitor

import (
	"testing"
	"time"
)

func TestRestartTrackerDoesNotReenterWithoutHeal(t *testing.T) {
	tracker := newRestartTracker(300, 3)
	base := time.Date(2026, time.February, 20, 20, 16, 57, 0, time.UTC)

	if _, entered := tracker.record("imapsync", base); entered {
		t.Fatal("first restart should not enter loop")
	}
	if _, entered := tracker.record("imapsync", base.Add(68*time.Second)); entered {
		t.Fatal("second restart should not enter loop")
	}
	if _, entered := tracker.record("imapsync", base.Add(136*time.Second)); !entered {
		t.Fatal("third restart should enter loop")
	}

	// A single long backoff gap prunes older timestamps, but the container is still
	// in the same loop until explicitly healed.
	if _, entered := tracker.record("imapsync", base.Add(413*time.Second)); entered {
		t.Fatal("existing loop should not re-enter after a long gap")
	}
	if _, entered := tracker.record("imapsync", base.Add(473*time.Second)); entered {
		t.Fatal("existing loop should not re-enter on subsequent restart")
	}
	if _, entered := tracker.record("imapsync", base.Add(533*time.Second)); entered {
		t.Fatal("existing loop should not re-enter without a heal")
	}
}
