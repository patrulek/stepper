package stepper_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/patrulek/stepper"
	"github.com/stretchr/testify/require"
)

const (
	new int32 = iota
	idle
	closed

	// transitive
	initializing
	executing
	closing
	renewing
)

func TestStep(t *testing.T) {
	state := stepper.New(new)
	newToClosed := stepper.NewTransition(
		closing,
		closed,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)

	// test step from new to closed, while in new
	err := state.Step(context.Background(), map[int32]any{
		new: newToClosed,
	})

	require.NoError(t, err)
	require.EqualValues(t, closed, state.Current())

	// test step from new to closed, while in closed
	err = state.Step(context.Background(), map[int32]any{
		new: newToClosed,
	})

	require.Error(t, err)

	// test step from closed to new, while in closed, but when context timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	closedToNew := stepper.NewTransition(
		closing,
		closed,
		func(fctx context.Context) error {
			<-fctx.Done()
			return fctx.Err()
		},
		func(err error) int32 {
			return closed
		},
	)

	err = state.Step(ctx, map[int32]any{
		closed: closedToNew,
	})
	cancel()

	require.EqualValues(t, context.DeadlineExceeded, err)
	require.EqualValues(t, closed, state.Current())

	// test step from closed to new, while in closed, but when context cancel
	ctx, cancel = context.WithCancel(context.Background())
	cancel()

	err = state.Step(ctx, map[int32]any{
		closed: closedToNew,
	})

	require.EqualValues(t, context.Canceled, err)
	require.EqualValues(t, closed, state.Current())
}

func TestConcurrentStep(t *testing.T) {
	state := stepper.New(new)
	sigC := make(chan struct{})

	go func() {
		newToClosedDelayed := stepper.NewTransition(
			closing,
			closed,
			func(context.Context) error {
				close(sigC)
				time.Sleep(5 * time.Second)
				return nil
			},
			func(error) int32 {
				return new
			},
		)

		// test step from new to closed, while in new
		err := state.Step(context.Background(), map[int32]any{
			new: newToClosedDelayed,
		})

		require.NoError(t, err)
		require.EqualValues(t, closed, state.Current())
	}()

	<-sigC
	errClosing := errors.New("closing")

	err := state.Step(context.Background(), map[int32]any{
		closing: errClosing,
	})

	require.EqualValues(t, stepper.ErrBusy, err)
	require.EqualValues(t, closing, state.Current())
}

func TestConcurrentQueueAndStep(t *testing.T) {
	state := stepper.New(new)
	sigC := make(chan struct{})

	newToNew := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)

	go func() {
		var err error
		for err != stepper.ErrBusy {
			err = state.Step(context.Background(), map[int32]any{
				new: newToNew,
			})
		}

		require.EqualValues(t, stepper.ErrBusy, err)
		require.EqualValues(t, new, state.Current())
		close(sigC)
	}()

	for sig := false; !sig; {
		select {
		case <-sigC:
			sig = true
		default:
			err := state.Queue(context.Background(), map[int32]any{
				new: newToNew,
			})
			require.NoError(t, err)
		}
	}
}

func TestQueue(t *testing.T) {
	state := stepper.New(new)
	sigC := make(chan struct{})

	newToClosed := stepper.NewTransition(
		closing,
		closed,
		func(ctx context.Context) error {
			close(sigC)
			time.Sleep(time.Second)
			return nil
		},
		func(err error) int32 {
			return new
		},
	)

	go func() {
		err := state.Queue(context.Background(), map[int32]any{
			new: newToClosed,
		})

		require.NoError(t, err)
		require.EqualValues(t, closed, state.Current())
	}()

	<-sigC

	newToClosed2 := stepper.NewTransition(
		closing,
		closed,
		func(ctx context.Context) error {
			time.Sleep(time.Second)
			return nil
		},
		func(err error) int32 {
			return new
		},
	)

	err := state.Queue(context.Background(), map[int32]any{
		new: newToClosed2,
	})

	require.EqualValues(t, stepper.ErrInvalidTransition, err)
	require.EqualValues(t, closed, state.Current())
}

func TestQueuePriority(t *testing.T) {
	state := stepper.New(new)
	sigC := make(chan struct{})
	queuedC := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	newToIdle := stepper.NewTransition(
		initializing,
		idle,
		func(ctx context.Context) error {
			close(sigC)
			time.Sleep(time.Second)
			return nil
		},
		func(err error) int32 {
			return new
		},
	)

	go func() {
		defer wg.Done()

		err := state.Step(context.Background(), map[int32]any{
			new: newToIdle,
		})

		require.NoError(t, err)
		require.True(t, state.Current() == idle || state.Current() == closing /* its possible that Queued step is already executing */)
	}()

	idleToIdle := stepper.NewTransition(
		executing,
		idle,
		func(ctx context.Context) error {
			return nil
		},
		func(err error) int32 {
			return idle
		},
	)

	go func() {
		defer wg.Done()
		<-queuedC

		err := state.Queue(context.Background(), map[int32]any{
			idle: idleToIdle,
		})

		require.EqualValues(t, stepper.ErrInvalidTransition, err)
		require.EqualValues(t, closed, state.Current())
	}()

	<-sigC

	idleToClosed := stepper.NewTransition(
		closing,
		closed,
		func(ctx context.Context) error {
			close(queuedC)
			time.Sleep(time.Second)
			return nil
		},
		func(err error) int32 {
			return idle
		},
	)

	err := state.QueueWithPriority(context.Background(), stepper.HighPriority, map[int32]any{
		idle: idleToClosed,
	})

	require.NoError(t, err)
	require.EqualValues(t, closed, state.Current())

	wg.Wait()
}

func TestCancel(t *testing.T) {
	state := stepper.New(new)
	sigC := make(chan struct{})
	queuedC := make(chan struct{})

	newToIdle := stepper.NewTransition(
		initializing,
		idle,
		func(fctx context.Context) error {
			close(sigC)
			<-fctx.Done()
			return fctx.Err()
		},
		func(err error) int32 {
			return new
		},
	)

	go func() {
		err := state.Step(context.Background(), map[int32]any{
			new: newToIdle,
		})

		require.EqualValues(t, context.Canceled, err)
		require.EqualValues(t, new, state.Current())
	}()

	<-sigC
	concurrent := 100

	go func() {
		defer close(queuedC)
		for state.Queued() < int32(concurrent+1) {
			time.Sleep(time.Millisecond)
		}
	}()

	newToClosed := stepper.NewTransition(
		closing,
		closed,
		func(ctx context.Context) error {
			return nil
		},
		func(err error) int32 {
			return new
		},
	)

	for i := 0; i < concurrent; i++ {
		go func(j int) {
			require.EqualValues(t, initializing, state.Current())
			err := state.Queue(context.Background(), map[int32]any{
				new: newToClosed,
			})

			require.EqualValues(t, stepper.ErrCanceled, err)
		}(i)
	}

	<-queuedC
	require.EqualValues(t, concurrent+1, state.Queued()) // +1 because of `Step` function
	state.Cancel()

	stepping := state.Stepping()
	current := state.Current()

	require.True(t, new == current || (stepping && initializing == current))
}
