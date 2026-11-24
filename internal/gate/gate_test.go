package gate

import (
	"errors"
	"testing"
)

func TestTheGateLetsOneJobInAtATime(t *testing.T) {
	var g Gate
	release, err := g.Enter(Scan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Enter(Execute)
	var busy *BusyError
	if !errors.As(err, &busy) || busy.Job != Scan || !errors.Is(err, ErrBusy) {
		t.Fatalf("second Enter = %v, want a BusyError naming the scan", err)
	}
	if err.Error() != Scan {
		t.Errorf("message = %q, want %q", err.Error(), Scan)
	}
	release()
	if _, err := g.Enter(Plan); err != nil {
		t.Errorf("after release: %v", err)
	}
}

func TestReleasingTwiceDoesNotFreeTheNextJob(t *testing.T) {
	var g Gate
	first, _ := g.Enter(Scan)
	first()
	if _, err := g.Enter(Plan); err != nil {
		t.Fatal(err)
	}
	first() // a second call of the first release must not free the plan's gate
	if _, err := g.Enter(Execute); !errors.Is(err, ErrBusy) {
		t.Errorf("Enter while the plan holds the gate = %v, want ErrBusy", err)
	}
}
