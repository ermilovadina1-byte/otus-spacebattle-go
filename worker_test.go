package spacebattle

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workerTestCommand func() error

func (command workerTestCommand) Execute() error { return command() }

func waitWorkerSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not signal completion")
	}
}

func TestStartWorkerWakesForCommand(t *testing.T) {
	w := NewCommandWorker()
	if err := (StartWorker{Worker: w}).Execute(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Started())
	select {
	case <-w.Done():
		t.Fatal("idle worker exited before a stop")
	default:
	}
	ran := make(chan struct{})
	if err := w.Enqueue(workerTestCommand(func() error { close(ran); return nil })); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, ran)
	if err := (SoftStopWorker{Worker: w}).Execute(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if err := (StartWorker{Worker: w}).Execute(); !errors.Is(err, ErrWorkerAlreadyStarted) {
		t.Fatalf("second start: %v", err)
	}
}

func TestHardStopWakesIdleWorkerAndRejectsWork(t *testing.T) {
	w := NewCommandWorker()
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Started())
	if err := (HardStopWorker{Worker: w}).Execute(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if err := w.Enqueue(workerTestCommand(func() error { return nil })); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("enqueue after stop: %v", err)
	}
}

func TestQueuedHardStopDiscardsLaterCommands(t *testing.T) {
	w := NewCommandWorker()
	var executed atomic.Int32
	if err := w.Enqueue(HardStopWorker{Worker: w}); err != nil {
		t.Fatal(err)
	}
	if err := w.Enqueue(workerTestCommand(func() error { executed.Add(1); return nil })); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if got := executed.Load(); got != 0 {
		t.Fatalf("commands after hard stop executed: %d", got)
	}
}

func TestHardStopDoesNotWaitForRunningCommand(t *testing.T) {
	w := NewCommandWorker()
	entered := make(chan struct{})
	release := make(chan struct{})
	var tailRan atomic.Bool
	if err := w.Enqueue(workerTestCommand(func() error {
		close(entered)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := w.Enqueue(workerTestCommand(func() error { tailRan.Store(true); return nil })); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, entered)
	if err := (HardStopWorker{Worker: w}).Execute(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
		t.Fatal("running command was interrupted")
	default:
	}
	close(release)
	waitWorkerSignal(t, w.Done())
	if tailRan.Load() {
		t.Fatal("hard stop executed a pending command")
	}
}

func TestSoftStopWaitsForRunningAndQueuedCommands(t *testing.T) {
	w := NewCommandWorker()
	entered := make(chan struct{})
	release := make(chan struct{})
	var tailRan atomic.Bool
	if err := w.Enqueue(workerTestCommand(func() error {
		close(entered)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := w.Enqueue(workerTestCommand(func() error { tailRan.Store(true); return nil })); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, entered)
	if err := (SoftStopWorker{Worker: w}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := w.Enqueue(workerTestCommand(func() error { return nil })); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("enqueue during drain: %v", err)
	}
	select {
	case <-w.Done():
		t.Fatal("soft stop finished while a command was still running")
	default:
	}
	close(release)
	waitWorkerSignal(t, w.Done())
	if !tailRan.Load() {
		t.Fatal("soft stop discarded an accepted command")
	}
}

func TestQueuedSoftStopDrainsLaterAcceptedCommands(t *testing.T) {
	w := NewCommandWorker()
	var executed atomic.Int32
	_ = w.Enqueue(SoftStopWorker{Worker: w})
	_ = w.Enqueue(workerTestCommand(func() error { executed.Add(1); return nil }))
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if got := executed.Load(); got != 1 {
		t.Fatalf("accepted commands executed: %d, want 1", got)
	}
}

func TestWorkerContinuesAfterErrorAndPanic(t *testing.T) {
	w := NewCommandWorker()
	wantErr := errors.New("failed command")
	var executed atomic.Int32
	_ = w.Enqueue(workerTestCommand(func() error { return wantErr }))
	_ = w.Enqueue(panicCommand{value: "broken command"})
	_ = w.Enqueue(workerTestCommand(func() error { executed.Add(1); return nil }))
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	if err := w.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if got := executed.Load(); got != 1 {
		t.Fatalf("commands after failure executed: %d, want 1", got)
	}
	failures := w.Failures()
	if len(failures) != 2 || !errors.Is(failures[0].Err, wantErr) || failures[1].Err == nil {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestWorkersHaveIndependentQueuesAndConcurrentProducers(t *testing.T) {
	blocked := NewCommandWorker()
	active := NewCommandWorker()
	entered := make(chan struct{})
	release := make(chan struct{})
	_ = blocked.Enqueue(workerTestCommand(func() error {
		close(entered)
		<-release
		return nil
	}))
	if err := blocked.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, entered)
	if err := active.Start(); err != nil {
		t.Fatal(err)
	}
	var producers sync.WaitGroup
	var executed atomic.Int32
	for i := 0; i < 50; i++ {
		producers.Add(1)
		go func() {
			defer producers.Done()
			if err := active.Enqueue(workerTestCommand(func() error { executed.Add(1); return nil })); err != nil {
				t.Errorf("enqueue: %v", err)
			}
		}()
	}
	producers.Wait()
	if err := active.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, active.Done())
	if got := executed.Load(); got != 50 {
		t.Fatalf("active worker executed %d commands, want 50", got)
	}
	close(release)
	if err := blocked.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, blocked.Done())
}

func TestExistingMoveRunsInCommandWorker(t *testing.T) {
	object, err := NewTorpedo(Vector{X: 12, Y: 5}, Vector{X: -7, Y: 3})
	if err != nil {
		t.Fatal(err)
	}
	w := NewCommandWorker()
	if err := w.Enqueue(Move{Object: object}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	if err := w.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, w.Done())
	if position, err := object.Position(); err != nil || position != (Vector{X: 5, Y: 8}) {
		t.Fatalf("position = %+v, err = %v", position, err)
	}
}
