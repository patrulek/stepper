// usync is a "sync utilities" package.
// This implementes generic wrapper over RWMutex.
package usync

import (
	"sync"
)

// Locked is a generic RWMutex lock over T object.
type Locked[T any] struct {
	lock   sync.RWMutex
	object T
}

// NewLocked returns new Locked[T] object.
func NewLocked[T any](locked T) *Locked[T] {
	return &Locked[T]{
		object: locked,
	}
}

// Read performs thread-safe read operation on underlying T object with given callback.
// It is user responsibility to ensure callback does not modify T object.
//
// Example usage:
//
// val := usync.NewLocked[int32](0).Read(
//
//	 func(i *int32) any {
//		return *i
//	 }
//
// ).(int32)
func (this *Locked[T]) Read(f func(*T) any) any {
	this.lock.RLock()
	res := f(&this.object)
	this.lock.RUnlock()
	return res
}

// Modify performs thread-safe write operation on underlying T object with given callback.
// Example usage:
//
// _ = usync.NewLocked[int32](0).Modify(
//
//	func(i *int32) any {
//	    *i += 1
//	    return struct{}{}
//	}
//
// )
func (this *Locked[T]) Modify(f func(*T) any) any {
	this.lock.Lock()
	res := f(&this.object)
	this.lock.Unlock()
	return res
}
