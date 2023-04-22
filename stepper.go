// Stepper is a package that implements thread-safe, in-memory finite state machine.
// Simple API allows for changing or queuing transitions for user-defined states.
package stepper

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/patrulek/stepper/internal/pqueue"
	"github.com/patrulek/stepper/internal/usync"
)

const (
	NoPriority int32 = iota
	NormalPriority
	HighPriority
)

var (
	// ErrBusy is returned when trying to change state through Step function but there is already transition that is in progress or queued.
	ErrBusy = errors.New("another transition in progress")

	// ErrCanceled is returned when queued transition was canceled by state machine through Cancel.
	ErrCanceled = errors.New("transition canceled")

	// ErrInvalidState is returned when trying to transit from invalid state (negative one).
	ErrInvalidState = errors.New("invalid machine state")

	// ErrInvalidTransition is returned when trying to transit from a state that was not specified in transition table.
	ErrInvalidTransition = errors.New("invalid transition")

	// ErrInvalidPriority is returned when trying to queue transition with invalid priority value.
	ErrInvalidPriority = errors.New("invalid priority")

	// ErrEmptyTransitionTable is returned when empty transition table was passed to Step or Queue functions.
	ErrEmptyTransitionTable = errors.New("empty transition table")
)

// TransitionMap maps machine state to an error or transition object.
type TransitionMap = map[int32]any

// Stepper represents a thread-safe, in-memory finite state machine.
type Stepper struct {
	state    int32
	stepping int32
	pqueue   *usync.Locked[pqueue.Queue]
	cancel   *usync.Locked[context.CancelFunc]
}

// transition represents state transition object.
// It holds information about transitive and final state
// and also callback function that will be called to perform transition
// and a rollback function to a safe state in a case transition function returned error.
// Transition function must be specified or program will panic.
// Rollback function may be nil, in that case state machine will perform no rollback and will bit-negate transitive state, making this negative value.
type transition struct {
	stepFunc   func(context.Context) error
	errFunc    func(error) int32
	transitive int32
	final      int32
}

// New returns a *Stepper object with initial state.
func New(initial int32) *Stepper {
	stepper := &Stepper{}
	stepper.cancel = usync.NewLocked(context.CancelFunc(func() {}))
	stepper.pqueue = usync.NewLocked(*pqueue.NewQueue())
	stepper.state = initial
	return stepper
}

// NewTransition returns transition object with given properties.
// Transitive and final states should be non-negative.
func NewTransition(transitive, final int32, stepFunc func(context.Context) error, errFunc func(error) int32) transition {
	if stepFunc == nil {
		panic("step function must be defined for Transition object")
	}

	if errFunc == nil {
		errFunc = func(err error) int32 {
			return ^transitive
		}
	}

	return transition{
		stepFunc:   stepFunc,
		errFunc:    errFunc,
		transitive: transitive,
		final:      final,
	}
}

// Step tries to make state transition. Transition will be done only if there are no other queued or in-progress transitions already.
// In that case returned error will be passed from transition function.
// Otherwise this will return immediately with ErrBusy error.
func (this *Stepper) Step(ctx context.Context, transitions TransitionMap) error {
	if len(transitions) == 0 {
		return ErrEmptyTransitionTable
	}

	if ok := this.pqueue.Modify(func(q *pqueue.Queue) any {
		return q.QueueIfEmpty()
	}).(bool); !ok {
		return ErrBusy
	}

	select {
	case <-ctx.Done():
		_ = this.pqueue.Modify(
			func(q *pqueue.Queue) any {
				q.Pop()
				return struct{}{}
			},
		)
		return ctx.Err()
	default:
		break
	}

	err := this.step(ctx, transitions)

	_ = this.pqueue.Modify(func(q *pqueue.Queue) any {
		q.Pop()
		return struct{}{}
	})

	return err
}

// Queue calls QueueWithPriority with NoPriority as argument.
func (this *Stepper) Queue(ctx context.Context, transitions TransitionMap) error {
	return this.QueueWithPriority(ctx, NoPriority, transitions)
}

// QueueWithPriority queues transition with given priority. In a case it's first queued transition
// it will make transition immediately, unless canceled. Otherwise it will perform transitions
// in FIFO manner from highest to lowest transitions priorities.
func (this *Stepper) QueueWithPriority(ctx context.Context, priority int32, transitions TransitionMap) error {
	if len(transitions) == 0 {
		return ErrEmptyTransitionTable
	}

	if priority < NoPriority || priority > HighPriority {
		return ErrInvalidPriority
	}

	signalC := this.pqueue.Modify(
		func(q *pqueue.Queue) any {
			return q.Queue(priority)
		},
	).(<-chan bool)

	select {
	case <-ctx.Done():
		_ = this.pqueue.Modify(
			func(q *pqueue.Queue) any {
				q.Unqueue(signalC)
				return struct{}{}
			},
		)
		return ctx.Err()
	case allowed, ok := <-signalC:
		if !ok || !allowed {
			return ErrCanceled
		}
	}

	err := this.step(ctx, transitions)

	_ = this.pqueue.Modify(func(q *pqueue.Queue) any {
		q.Pop()
		return struct{}{}
	})

	return err
}

// step performs state transition. Should be called only from already synchronised methods, eg. Step or Queue.
func (this *Stepper) step(ctx context.Context, transitions TransitionMap) error {
	if this.state < 0 {
		return ErrInvalidState
	}

	v, ok := transitions[this.state]
	if !ok {
		return ErrInvalidTransition
	}

	switch vt := v.(type) {
	case transition:
		ctx, cancel := context.WithCancel(ctx)
		_ = this.cancel.Modify(func(fcancel *context.CancelFunc) any {
			*fcancel = cancel
			return struct{}{}
		})

		atomic.StoreInt32(&this.stepping, 1)
		atomic.StoreInt32(&this.state, vt.transitive)

		if err := vt.stepFunc(ctx); err != nil {
			safeState := vt.errFunc(err)
			atomic.StoreInt32(&this.state, safeState)
			atomic.StoreInt32(&this.stepping, 0)

			_ = this.cancel.Modify(func(fcancel *context.CancelFunc) any {
				(*fcancel)()
				*fcancel = func() {}
				return struct{}{}
			})
			return err
		}

		atomic.StoreInt32(&this.state, vt.final)
		atomic.StoreInt32(&this.stepping, 0)

		_ = this.cancel.Modify(func(fcancel *context.CancelFunc) any {
			(*fcancel)()
			*fcancel = func() {}
			return struct{}{}
		})
		return nil
	case error:
		return vt
	case nil:
		return nil
	default:
		panic("state transition must be Transition object or error")
	}
}

// Current returns current machine state.
func (this *Stepper) Current() int32 {
	return atomic.LoadInt32(&this.state)
}

// Queued returns number of queued transitions.
func (this *Stepper) Queued() int32 {
	return this.pqueue.Read(func(q *pqueue.Queue) any { return q.Count() }).(int32)
}

// Stepping returns true if transition is currently in progress, false otherwise.
func (this *Stepper) Stepping() bool {
	return atomic.LoadInt32(&this.stepping) > 0
}

// Cancel clears transition queue and tries to stop current transition by canceling its context.
func (this *Stepper) Cancel() {
	_ = this.pqueue.Modify(func(q *pqueue.Queue) any {
		q.Clear()
		return struct{}{}
	})

	if !this.Stepping() {
		return
	}

	_ = this.cancel.Modify(func(fcancel *context.CancelFunc) any {
		(*fcancel)()
		*fcancel = func() {}
		return struct{}{}
	})
}
