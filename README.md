# Stepper

## Overview

Stepper is a thread-safe, in-memory finite state machine written in Golang.

Aim of this package is to provide simple API that will let you manage your objects states in a safe manner.

## Quick start

1. Define states for your "machine"
2. Define transitions that will be executed on input (function calls)
3. Profit

```go
import (
    "context"
    "errors"

    "github.com/patrulek/stepper"
)

const (
    // final states
    idle = iota
    closed

    // transitive states
    processing
    closing
)

var state = stepper.New(idle)
var ErrClosed = errors.New("closed")

func heavyTask(context.Context) error {
    // much heavy
    return nil
}

func rollbackHeavyTask() {
    // rollback
}

func Process(ctx context.Context) error {
    transition := stepper.NewTransition(
        processing,
        idle,
        func(fctx context.Context) error {
            return heavyTask(fctx)
        },
        func(err error) int32 {
            rollbackHeavyTask()
            return idle
        },
    )

    return state.Step(ctx, map[int32]any{
        closed: ErrClosed,
        idle:   transition,
    })
}

func Close(ctx context.Context) error {
    transition := stepper.NewTransition(
        closing,
        closed,
        func(fctx context.Context) error {
            // release resources
            return nil
        },
        nil, // in a case when transition fails, stepper will set invalid, non-functional state (unless proper repair function defined)
    )

    return state.Queue(ctx, map[int32]any{
        closed: ErrClosed,
        idle:   transition,
    })
}
```

## API

Stepper expose three functions that let you change states:

- `Step(context.Context, TransitionMap) error`: Performs transition only if no other transition is queued or currently in progress, otherwise immediately return an error
- `Queue(context.Context, TransitionMap) error`: Queues given transition with lowest possible priority; if it's first element in queue transition will execute immediately
- `QueueWithPriority(context.Context, int32, TransitionMap) error`: Queues transition with given priority; if it's first element in queue transition will execute immediately

Error returned by any of these methods may be validation cancellation or state transition error.

`Priority` can takes one of three values:

- stepper.NoPriority
- stepper.NormalPriority
- stepper.HighPriority

Any other values will return error.

`TransitionMap` is in fact an alias for `map[int32]any`, but only these types will work:

- `stepper.transition`: stepper will perform transition for a key that matches current machine state; this will panic if transition function was not defined
- `error`: stepper will return given error for a key that matches current machine state
- `nil`: stepper will return nil for a key that matches current machine state

Any other type will yield panic.

## Changelog

- **v0.1.0 - 22.04.2023**: Initial version

```console
-------------------------------------------------------------------------------
Language                     files          blank        comment           code
-------------------------------------------------------------------------------
Go                               5            180            100            810
Markdown                         1             29              0             93
YAML                             2             10              1             84
-------------------------------------------------------------------------------
TOTAL                            8            219            101            987
-------------------------------------------------------------------------------
```
