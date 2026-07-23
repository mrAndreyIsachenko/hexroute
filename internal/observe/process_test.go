package observe

import (
	"context"
	"testing"
)

func TestSingBoxObservationDoesNotTakeOwnership(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(psCommand, "-axo", "pid=,ppid=,uid=,comm="): []byte(
			"  100     1     0 /usr/libexec/something\n" +
				"  321   200     0 /opt/local/bin/sing-box\n",
		),
	}}
	observer, err := NewProcessObserver(runner)
	if err != nil {
		t.Fatalf("NewProcessObserver() error: %v", err)
	}

	observation, err := observer.SingBox(context.Background(), 999)
	if err != nil {
		t.Fatalf("SingBox() error: %v", err)
	}
	if !observation.Running || observation.Process.PID != 321 || observation.OwnedChild {
		t.Fatalf("SingBox() = %+v", observation)
	}

	owned, err := observer.SingBox(context.Background(), 200)
	if err != nil {
		t.Fatalf("SingBox(expected child) error: %v", err)
	}
	if !owned.OwnedChild {
		t.Fatalf("SingBox(expected child) = %+v", owned)
	}
}

func TestSingBoxObservationReturnsNotRunning(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(psCommand, "-axo", "pid=,ppid=,uid=,comm="): []byte(
			"  100     1     0 /usr/libexec/something\n",
		),
	}}
	observer, _ := NewProcessObserver(runner)

	observation, err := observer.SingBox(context.Background(), 0)
	if err != nil {
		t.Fatalf("SingBox() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("SingBox() = %+v, want not running", observation)
	}
}
