// Package gate lets one scan, plan building or plan execution run at a time (spec 6.11).
// A plan built during a scan would compare against a scan that is about to change, and a
// scan or a plan during an execution would read the apps halfway through a deletion.
package gate

import (
	"errors"
	"sync"
)

// The jobs that share the gate. Each is the message a refused request gets.
const (
	Scan    = "A scan is running."
	Plan    = "A plan is being built."
	Execute = "A plan is being executed."
)

// ErrBusy matches every error Enter returns.
var ErrBusy = errors.New("busy")

// BusyError says which job holds the gate.
type BusyError struct{ Job string }

func (e *BusyError) Error() string        { return e.Job }
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Gate admits one job at a time. The zero value is free.
type Gate struct {
	mu  sync.Mutex
	job string
}

// Enter takes the gate for job, or returns a *BusyError naming the job that holds it.
// release frees the gate; calling it again does nothing, so it cannot free a later job.
func (g *Gate) Enter(job string) (release func(), err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != "" {
		return nil, &BusyError{Job: g.job}
	}
	g.job = job
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.job = ""
			g.mu.Unlock()
		})
	}, nil
}
