package benchmarks_test

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/patrulek/stepper"
)

const (
	new int32 = iota
	closed

	// transitive
	closing
	renewing
)

func BenchmarkStepDenied(b *testing.B) {
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)

	serial := 250
	table := map[int32]any{
		closed: transition,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := 0; j < serial; j++ {
			_ = state.Step(context.Background(), table)
		}
	}
}

func BenchmarkStepPermitted(b *testing.B) {
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)

	serial := 250
	table := map[int32]any{
		new: transition,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := 0; j < serial; j++ {
			_ = state.Step(context.Background(), table)
		}
	}
}

func BenchmarkQueuePermitted(b *testing.B) {
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)

	serial := 250
	table := map[int32]any{
		new: transition,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := 0; j < serial; j++ {
			_ = state.Queue(context.Background(), table)
		}
	}
}

func BenchmarkConcurrentQueue(b *testing.B) {
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error { return nil },
		func(err error) int32 { return new },
	)
	table := map[int32]any{
		new: transition,
	}
	concurrent := 250

	b.ResetTimer()
	defer func() {
		b.StopTimer()
		runtime.GC()
		b.StartTimer()
	}()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(concurrent)

		for j := 0; j < concurrent; j++ {
			go func() {
				defer wg.Done()
				_ = state.Queue(context.Background(), table)
			}()
		}

		wg.Wait()
	}
}

func BenchmarkConcurrentQueueWithCompute(b *testing.B) {
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(ctx context.Context) error {
			t0 := time.Now()
			cnt := 0
			for time.Since(t0) < 100*time.Microsecond {
				cnt = (cnt + 1) % 2
			}
			return nil
		},
		func(err error) int32 { return new },
	)

	table := map[int32]any{
		new: transition,
	}
	concurrent := 250

	b.ResetTimer()
	defer func() {
		b.StopTimer()
		runtime.GC()
		b.StartTimer()
	}()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(concurrent)

		for j := 0; j < concurrent; j++ {
			go func() {
				defer wg.Done()
				_ = state.Queue(context.Background(), table)
			}()
		}

		wg.Wait()
	}
}

func BenchmarkCancel(b *testing.B) {
	cancelC := make(chan struct{})
	lockC := make(chan struct{})
	state := stepper.New(new)
	transition := stepper.NewTransition(
		renewing,
		new,
		func(fctx context.Context) error {
			<-lockC
			close(cancelC)
			<-fctx.Done()
			return fctx.Err()
		},
		func(err error) int32 { return new },
	)

	table := map[int32]any{
		new: transition,
	}
	concurrent := 25

	b.ResetTimer()
	defer func() {
		b.StopTimer()
		runtime.GC()
		b.StartTimer()
	}()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cancelC = make(chan struct{})
		lockC = make(chan struct{})

		// wait till all tasks will be queued
		go func() {
			defer close(lockC)
			for state.Queued() < int32(concurrent) {
				time.Sleep(time.Millisecond)
			}
		}()

		var wg sync.WaitGroup
		wg.Add(concurrent)

		for j := 0; j < concurrent; j++ {
			go func(j int) {
				defer wg.Done()
				_ = state.Queue(context.Background(), table)
			}(j)
		}

		<-cancelC
		b.StartTimer()
		state.Cancel()
		wg.Wait()
	}
}
