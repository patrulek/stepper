// pqueue is a simple thread-unsafe priority queue implementation.
package pqueue

const (
	noPriority int32 = iota
	normalPriority
	highPriority

	maxPriority
)

// Queue represents a simple priority queue with three priority levels.
// Holds a slice of bool channels to signal next allowed elements in queue.
//
// This structure doesn't have any synchronisation mechanism so to ensure thread safety
// it's needed for user to synchronise access to this structure.
//
// Arguments for methods of this structure aren't validated, so it's needed for a caller to ensure proper usage.
type Queue struct {
	queues     [maxPriority]int32
	priority   int32
	priorities []int32
	signals    []chan bool
	cleared    bool
}

// NewQueue returns *Queue object.
func NewQueue() *Queue {
	return &Queue{
		signals:    make([]chan bool, 0, 32),
		priorities: make([]int32, 0, 32),
	}
}

// QueueIfEmpty calls Queue if queue is already empty.
// Returns true on successful call to Queue, otherwise false.
func (this *Queue) QueueIfEmpty() bool {
	if !this.Empty() {
		return false
	}

	_ = this.Queue(noPriority)
	return true
}

// Queue puts new task into a queue with given priority.
// If this is first element in queue then returned signal is already closed instead of returning nil for convenience.
// If task priority is higher than current max set priority, the priority of queue is updated.
// New signal channel is placed in proper place depending on current state of queue and priority of new task:
//   - if the priority is the highest or if it's the second element in queue, signal of this task is inserted at the front of the queue
//   - otherwise signal is inserted somewhere in the middle of queue, depending on current state and priority of new task
func (this *Queue) Queue(priority int32) (signalC <-chan bool) {
	if this.Empty() {
		this.queues[priority]++
		signalC := make(chan bool, 1)
		signalC <- true
		close(signalC)

		this.priorities = append(this.priorities, priority)
		this.signals = append(this.signals, signalC)

		return signalC
	}

	pos := int32(0)

	if priority > this.priority {
		this.priority = priority
		pos = 1
	} else if priority == this.priority {
		pos = this.queues[priority]
	} else {
		for prio := this.priority; prio >= priority; prio-- {
			pos += this.queues[prio]
		}
	}

	this.queues[priority]++
	this.priorities = append(this.priorities, priority)

	elem := make(chan bool, 1)

	head := this.signals[:pos]
	tail := this.signals[pos:]
	this.signals = append(append(head, elem), tail...)
	return this.signals[pos]
}

// Unqueue removes task given by provided signal channel.
// If given task was already canceled by state machine function just returns.
// Otherwise it searches for this task in a queue comparing signal channels and if signal is found
// it consume this task updating queue max priority if necessary.
// In a case when task was not found, it is because previous task already signalled current one to execute, so we just call Pop()
// because we are now in front of queue.
func (this *Queue) Unqueue(signalC <-chan bool) {
	select {
	case _, open := <-signalC:
		if !open {
			return
		}
	default:
		break
	}

	for i := range this.signals {
		if this.signals[i] == signalC {
			taskPriority := this.priorities[i]

			this.signals = append(this.signals[:i], this.signals[i+1:]...)
			this.priorities = append(this.priorities[:i], this.priorities[i+1:]...)

			this.queues[taskPriority]--
			if taskPriority < this.priority {
				return
			}

			for this.priority > noPriority && this.queues[this.priority] == 0 {
				this.priority--
			}

			return
		}
	}

	this.Pop()
}

// Pop signals next task in queue so it may start execution.
// In a case state machine cancelled all tasks, this resets the `cleared` flag and just returns.
// Otherwise this consumes task and updates queue max priority if necessary.
func (this *Queue) Pop() {
	if this.cleared {
		this.cleared = false
		return
	}

	taskPriority := this.priorities[0]
	this.queues[taskPriority]--

	this.signals = this.signals[1:]
	this.priorities = this.priorities[1:]

	if len(this.signals) > 0 {
		this.signals[0] <- true
		close(this.signals[0])
	}

	if taskPriority < this.priority {
		return
	}

	for this.priority > noPriority && this.queues[this.priority] == 0 {
		this.priority--
	}
}

// Count returns number of queued tasks.
func (this *Queue) Count() int32 {
	return this.queues[noPriority] + this.queues[normalPriority] + this.queues[highPriority]
}

// Empty returns true if no tasks were queued, false otherwise.
func (this *Queue) Empty() bool {
	return this.queues[noPriority] == 0 && this.queues[normalPriority] == 0 && this.queues[highPriority] == 0
}

// Clear sets `cleared` flag, closes all signal channels and reset queue state.
// Cleared flag is used for Pop function to stop consumption of currently executing task.
// Do nothing if queue is empty.
func (this *Queue) Clear() {
	if this.Empty() {
		return
	}

	// mark for pop()
	this.cleared = true

	// release all queued tasks channels
	for _, signalC := range this.signals[1:] {
		close(signalC)
	}

	this.signals = this.signals[:0]
	this.priorities = this.priorities[:0]

	// remove all tasks
	this.priority = noPriority
	this.queues[noPriority] = 0
	this.queues[normalPriority] = 0
	this.queues[highPriority] = 0
}
