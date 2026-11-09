package spacebattle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type scriptedCommand struct {
	calls int
	fail  int
	err   error
}

func (command *scriptedCommand) Execute() error {
	command.calls++
	if command.calls <= command.fail {
		return fmt.Errorf("attempt %d: %w", command.calls, command.err)
	}
	return nil
}

type panicCommand struct{ value any }

func (command panicCommand) Execute() error { panic(command.value) }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestQueueIsFIFOAndRejectsNil(t *testing.T) {
	queue := &CommandQueue{}
	if err := queue.Enqueue(nil); !errors.Is(err, ErrNilCommand) {
		t.Fatalf("enqueue nil: %v", err)
	}
	first, second := &scriptedCommand{}, &scriptedCommand{}
	_ = queue.Enqueue(first)
	_ = queue.Enqueue(second)
	for _, want := range []Command{first, second} {
		got, ok := queue.Dequeue()
		if !ok || got != want {
			t.Fatalf("dequeue: got %v, want %v", got, want)
		}
	}
	if _, ok := queue.Dequeue(); ok || queue.Len() != 0 {
		t.Fatal("queue should be empty")
	}
}

func TestRuleSelectsCommandAndWrappedError(t *testing.T) {
	queue := &CommandQueue{}
	first := &scriptedCommand{fail: 1, err: ErrNilObject}
	second := &scriptedCommand{fail: 1, err: ErrInvalidVector}
	_ = queue.Enqueue(first)
	_ = queue.Enqueue(second)
	matched := 0
	processor := Processor{Queue: queue, Rules: []ErrorRule{{
		Match: func(command Command, err error) bool {
			return command == second && errors.Is(err, ErrInvalidVector)
		},
		Handle: func(command Command, err error) error {
			matched++
			return nil
		},
	}}}
	if processed, err := processor.RunNext(); !processed || !errors.Is(err, ErrNilObject) {
		t.Fatalf("unmatched error: processed=%v, err=%v", processed, err)
	}
	if processed, err := processor.RunNext(); !processed || err != nil || matched != 1 {
		t.Fatalf("matched error: processed=%v, err=%v, matched=%d", processed, err, matched)
	}
}

func TestLoggingCommandAndHandler(t *testing.T) {
	queue := &CommandQueue{}
	var log bytes.Buffer
	failed := Move{}
	if err := EnqueueLog(queue, &log)(failed, ErrNilObject); err != nil {
		t.Fatal(err)
	}
	if queue.Len() != 1 || log.Len() != 0 {
		t.Fatal("handler must enqueue without logging immediately")
	}
	processor := Processor{Queue: queue}
	if err := processor.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "spacebattle.Move: game object is nil") {
		t.Fatalf("log lacks command or error: %q", log.String())
	}
	if err := (&LogFailure{Failed: failed, Cause: ErrNilObject, Writer: failingWriter{}}).Execute(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writer error lost: %v", err)
	}
}

func TestRetryHandlerEnqueuesWithoutExecuting(t *testing.T) {
	queue := &CommandQueue{}
	failed := &scriptedCommand{fail: 1, err: ErrNilObject}
	if err := EnqueueRetry(queue)(failed, ErrNilObject); err != nil {
		t.Fatal(err)
	}
	if failed.calls != 0 || queue.Len() != 1 {
		t.Fatal("retry must be deferred")
	}
	if err := (&Processor{Queue: queue}).Run(); err == nil {
		t.Fatal("failed retry should return an unhandled error")
	}
	if failed.calls != 1 {
		t.Fatalf("calls: %d", failed.calls)
	}
}

func TestRetryOnceThenLog(t *testing.T) {
	for _, test := range []struct {
		name     string
		fail     int
		wantLog  bool
		wantCall int
	}{
		{"first attempt succeeds", 0, false, 1},
		{"retry succeeds", 1, false, 2},
		{"retry fails", 2, true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			queue := &CommandQueue{}
			var log bytes.Buffer
			command := &scriptedCommand{fail: test.fail, err: ErrNilObject}
			_ = queue.Enqueue(command)
			processor := Processor{Queue: queue, Rules: RetryOnceRules(queue, &log)}
			if err := processor.Run(); err != nil {
				t.Fatal(err)
			}
			if command.calls != test.wantCall || (log.Len() > 0) != test.wantLog {
				t.Fatalf("calls=%d, log=%q", command.calls, log.String())
			}
			if test.wantLog && !strings.Contains(log.String(), "attempt 2") {
				t.Fatalf("log must contain final error: %q", log.String())
			}
		})
	}
}

func TestRetryTwiceThenLog(t *testing.T) {
	for _, test := range []struct {
		fail     int
		wantCall int
		wantLog  bool
	}{
		{0, 1, false}, {1, 2, false}, {2, 3, false}, {3, 3, true},
	} {
		t.Run(fmt.Sprint(test.fail), func(t *testing.T) {
			queue := &CommandQueue{}
			var log bytes.Buffer
			command := &scriptedCommand{fail: test.fail, err: ErrInvalidVector}
			_ = queue.Enqueue(command)
			processor := Processor{Queue: queue, Rules: RetryTwiceRules(queue, &log)}
			if err := processor.Run(); err != nil {
				t.Fatal(err)
			}
			if command.calls != test.wantCall || (log.Len() > 0) != test.wantLog {
				t.Fatalf("calls=%d, log=%q", command.calls, log.String())
			}
			if test.wantLog && !strings.Contains(log.String(), "attempt 3") {
				t.Fatalf("log must contain third failure: %q", log.String())
			}
		})
	}
}

func TestPanicIsCaughtAtWorkerBoundary(t *testing.T) {
	for _, value := range []any{ErrNilObject, "unexpected state"} {
		queue := &CommandQueue{}
		_ = queue.Enqueue(panicCommand{value})
		var log bytes.Buffer
		processor := Processor{Queue: queue, Rules: RetryOnceRules(queue, &log)}
		if err := processor.Run(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(log.String(), "command panic") {
			t.Fatalf("panic not logged: %q", log.String())
		}
	}
}

func TestLogFailureIsNotRetriedForever(t *testing.T) {
	queue := &CommandQueue{}
	command := &scriptedCommand{fail: 2, err: ErrNilObject}
	_ = queue.Enqueue(command)
	processor := Processor{Queue: queue, Rules: RetryOnceRules(queue, failingWriter{})}
	if err := processor.Run(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("logging error should reach caller: %v", err)
	}
	if command.calls != 2 || queue.Len() != 0 {
		t.Fatalf("unexpected retry: calls=%d, queued=%d", command.calls, queue.Len())
	}
}

func TestExistingMoveCommandRunsInQueue(t *testing.T) {
	torpedo, err := NewTorpedo(Vector{X: 12, Y: 5}, Vector{X: -7, Y: 3})
	if err != nil {
		t.Fatal(err)
	}
	queue := &CommandQueue{}
	_ = queue.Enqueue(Move{Object: torpedo})
	if err := (&Processor{Queue: queue}).Run(); err != nil {
		t.Fatal(err)
	}
	position, _ := torpedo.Position()
	if position != (Vector{X: 5, Y: 8}) {
		t.Fatalf("position: %+v", position)
	}
}

func TestExistingMoveErrorSelectsRule(t *testing.T) {
	queue := &CommandQueue{}
	_ = queue.Enqueue(Move{})
	matched := false
	processor := Processor{Queue: queue, Rules: []ErrorRule{{
		Match: func(command Command, err error) bool {
			_, isMove := command.(Move)
			return isMove && errors.Is(err, ErrNilObject)
		},
		Handle: func(Command, error) error {
			matched = true
			return nil
		},
	}}}
	if err := processor.Run(); err != nil || !matched {
		t.Fatalf("old command should use new handler: matched=%v, err=%v", matched, err)
	}
}
