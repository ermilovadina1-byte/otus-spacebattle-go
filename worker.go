package spacebattle

import (
	"errors"
	"sync"
)

var (
	ErrNilWorker            = errors.New("command worker is nil")
	ErrWorkerAlreadyStarted = errors.New("command worker already started")
	ErrWorkerNotStarted     = errors.New("command worker has not started")
	ErrWorkerClosed         = errors.New("command worker is closed to new commands")
)

// WorkerFailure records a command error or a panic converted to an error.
type WorkerFailure struct {
	Command Command
	Err     error
}

// CommandWorker owns a FIFO queue and executes its commands in one goroutine.
// Different workers have independent queues. Create it with NewCommandWorker.
type CommandWorker struct {
	mu       sync.Mutex
	ready    *sync.Cond
	queue    []Command
	started  bool
	draining bool
	halted   bool
	failures []WorkerFailure
	startCh  chan struct{}
	doneCh   chan struct{}
}

func NewCommandWorker() *CommandWorker {
	w := &CommandWorker{
		startCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	w.ready = sync.NewCond(&w.mu)
	return w
}

// Enqueue is safe for concurrent producers and rejects commands after a stop.
func (w *CommandWorker) Enqueue(command Command) error {
	if w == nil {
		return ErrNilWorker
	}
	if command == nil {
		return ErrNilCommand
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.draining || w.halted {
		return ErrWorkerClosed
	}
	w.queue = append(w.queue, command)
	w.ready.Signal()
	return nil
}

// Start launches the single consumer. Commands may be queued before Start.
func (w *CommandWorker) Start() error {
	if w == nil {
		return ErrNilWorker
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return ErrWorkerAlreadyStarted
	}
	w.started = true
	go w.run()
	return nil
}

// HardStop rejects new work and discards pending commands. It cannot interrupt
// a command that is already executing; Done closes after that command returns.
func (w *CommandWorker) HardStop() error {
	if w == nil {
		return ErrNilWorker
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return ErrWorkerNotStarted
	}
	w.halted = true
	for i := range w.queue {
		w.queue[i] = nil
	}
	w.queue = nil
	w.ready.Broadcast()
	return nil
}

// SoftStop closes intake and lets all previously accepted commands finish.
func (w *CommandWorker) SoftStop() error {
	if w == nil {
		return ErrNilWorker
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return ErrWorkerNotStarted
	}
	w.draining = true
	w.ready.Broadcast()
	return nil
}

// Started closes when the worker goroutine enters its execution loop.
func (w *CommandWorker) Started() <-chan struct{} { return w.startCh }

// Done closes after the worker exits. Wait on it outside a worker command.
func (w *CommandWorker) Done() <-chan struct{} { return w.doneCh }

// Failures returns a snapshot. The educational worker retains every failure.
func (w *CommandWorker) Failures() []WorkerFailure {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]WorkerFailure(nil), w.failures...)
}

func (w *CommandWorker) run() {
	close(w.startCh)
	defer close(w.doneCh)
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && !w.draining && !w.halted {
			w.ready.Wait()
		}
		if w.halted || (w.draining && len(w.queue) == 0) {
			w.mu.Unlock()
			return
		}
		command := w.queue[0]
		w.queue[0] = nil
		w.queue = w.queue[1:]
		w.mu.Unlock()

		if err := executeSafely(command); err != nil {
			w.mu.Lock()
			w.failures = append(w.failures, WorkerFailure{Command: command, Err: err})
			w.mu.Unlock()
		}
	}
}

// StartWorker, HardStopWorker and SoftStopWorker fit the existing Command API.
// StartWorker runs outside its target worker; either stop command may be queued.
type StartWorker struct{ Worker *CommandWorker }

func (command StartWorker) Execute() error { return command.Worker.Start() }

type HardStopWorker struct{ Worker *CommandWorker }

func (command HardStopWorker) Execute() error { return command.Worker.HardStop() }

type SoftStopWorker struct{ Worker *CommandWorker }

func (command SoftStopWorker) Execute() error { return command.Worker.SoftStop() }

var _ Command = StartWorker{}
var _ Command = HardStopWorker{}
var _ Command = SoftStopWorker{}
