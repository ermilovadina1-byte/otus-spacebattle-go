package spacebattle

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestHardStopInMoveToStateEndsSourceAndDiscardsTail(t *testing.T) {
	source := NewCommandWorker()
	target := NewCommandWorker()
	var tailRan atomic.Bool
	for _, command := range []Command{
		MoveToCommand{Target: target},
		HardStopWorker{Worker: source},
		workerTestCommand(func() error { tailRan.Store(true); return nil }),
	} {
		if err := source.Enqueue(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, source.Done())
	if err := source.Enqueue(RunCommand{}); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("enqueue after hard stop: %v", err)
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	if err := target.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, target.Done())
	if tailRan.Load() {
		t.Fatal("hard stop forwarded or executed the pending command")
	}
}

func TestMoveToCommandForwardsStartingWithNextCommand(t *testing.T) {
	source := NewCommandWorker()
	target := NewCommandWorker()
	var ran atomic.Bool
	for _, command := range []Command{
		MoveToCommand{Target: target},
		workerTestCommand(func() error { ran.Store(true); return nil }),
		SoftStopWorker{Worker: source},
	} {
		if err := source.Enqueue(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, source.Done())
	if ran.Load() {
		t.Fatal("source executed a command that should have been forwarded")
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	if err := target.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, target.Done())
	if !ran.Load() {
		t.Fatal("target did not execute the forwarded command")
	}
}

func TestRunCommandRestoresLocalExecutionStartingWithNextCommand(t *testing.T) {
	source := NewCommandWorker()
	target := NewCommandWorker()
	var forwarded, local atomic.Bool
	for _, command := range []Command{
		MoveToCommand{Target: target},
		workerTestCommand(func() error { forwarded.Store(true); return nil }),
		RunCommand{},
		workerTestCommand(func() error { local.Store(true); return nil }),
		SoftStopWorker{Worker: source},
	} {
		if err := source.Enqueue(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, source.Done())
	if !local.Load() || forwarded.Load() {
		t.Fatalf("before target start: local=%t forwarded=%t", local.Load(), forwarded.Load())
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	if err := target.SoftStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, target.Done())
	if !forwarded.Load() {
		t.Fatal("target did not execute the earlier forwarded command")
	}
}

func TestInvalidMoveToTargetKeepsRunningState(t *testing.T) {
	source := NewCommandWorker()
	var ran atomic.Bool
	for _, command := range []Command{
		MoveToCommand{Target: source},
		workerTestCommand(func() error { ran.Store(true); return nil }),
		SoftStopWorker{Worker: source},
	} {
		if err := source.Enqueue(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, source.Done())
	if !ran.Load() {
		t.Fatal("invalid target changed the state")
	}
	if failures := source.Failures(); len(failures) != 1 || !errors.Is(failures[0].Err, ErrMoveToTarget) {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestClosedMoveToTargetRecordsFailureAndCanResume(t *testing.T) {
	source := NewCommandWorker()
	target := NewCommandWorker()
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	if err := target.HardStop(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, target.Done())
	var local atomic.Bool
	for _, command := range []Command{
		MoveToCommand{Target: target},
		workerTestCommand(func() error { return nil }),
		RunCommand{},
		workerTestCommand(func() error { local.Store(true); return nil }),
		SoftStopWorker{Worker: source},
	} {
		if err := source.Enqueue(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	waitWorkerSignal(t, source.Done())
	if !local.Load() {
		t.Fatal("source did not resume local execution")
	}
	if failures := source.Failures(); len(failures) != 1 || !errors.Is(failures[0].Err, ErrWorkerClosed) {
		t.Fatalf("failures = %+v", failures)
	}
}
