package spacebattle

import (
	"fmt"
	"io"
)

// LogFailure writes the command type and the failure that reached the worker.
type LogFailure struct {
	Failed Command
	Cause  error
	Writer io.Writer
}

func (command *LogFailure) Execute() error {
	if command.Writer == nil {
		return fmt.Errorf("log writer is nil")
	}
	_, err := fmt.Fprintf(command.Writer, "%T: %v\n", command.Failed, command.Cause)
	return err
}

// Retry repeats the original operation once when the worker executes it.
type Retry struct{ Original Command }

func (command *Retry) Execute() error { return command.Original.Execute() }

// RetryTwice is a distinct stage so rules can identify a second failed retry.
type RetryTwice struct{ Original Command }

func (command *RetryTwice) Execute() error { return command.Original.Execute() }

func EnqueueLog(queue *CommandQueue, writer io.Writer) func(Command, error) error {
	return func(failed Command, cause error) error {
		return queue.Enqueue(&LogFailure{Failed: failed, Cause: cause, Writer: writer})
	}
}

func EnqueueRetry(queue *CommandQueue) func(Command, error) error {
	return func(failed Command, _ error) error {
		return queue.Enqueue(&Retry{Original: failed})
	}
}

func enqueueSecondRetry(queue *CommandQueue) func(Command, error) error {
	return func(failed Command, _ error) error {
		return queue.Enqueue(&RetryTwice{Original: failed.(*Retry).Original})
	}
}

// RetryOnceRules retries an initial failure, then logs a failed retry.
func RetryOnceRules(queue *CommandQueue, writer io.Writer) []ErrorRule {
	return []ErrorRule{
		{Match: isRetry, Handle: EnqueueLog(queue, writer)},
		{Match: isInitial, Handle: EnqueueRetry(queue)},
	}
}

// RetryTwiceRules retries an initial failure twice, then logs it.
func RetryTwiceRules(queue *CommandQueue, writer io.Writer) []ErrorRule {
	return []ErrorRule{
		{Match: isSecondRetry, Handle: EnqueueLog(queue, writer)},
		{Match: isRetry, Handle: enqueueSecondRetry(queue)},
		{Match: isInitial, Handle: EnqueueRetry(queue)},
	}
}

func isRetry(command Command, _ error) bool {
	_, ok := command.(*Retry)
	return ok
}

func isSecondRetry(command Command, _ error) bool {
	_, ok := command.(*RetryTwice)
	return ok
}

func isInitial(command Command, _ error) bool {
	switch command.(type) {
	case *Retry, *RetryTwice, *LogFailure:
		return false
	default:
		return true
	}
}

var _ Command = Move{}
var _ Command = Rotate{}
var _ Command = (*LogFailure)(nil)
var _ Command = (*Retry)(nil)
var _ Command = (*RetryTwice)(nil)
