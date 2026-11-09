package spacebattle

import (
	"errors"
	"fmt"
)

// Command is the common contract for game operations and follow-up actions.
type Command interface {
	Execute() error
}

var ErrNilCommand = errors.New("command is nil")

// CommandQueue keeps commands in FIFO order. It is used by one game worker.
type CommandQueue struct {
	commands []Command
}

func (q *CommandQueue) Enqueue(command Command) error {
	if command == nil {
		return ErrNilCommand
	}
	q.commands = append(q.commands, command)
	return nil
}

func (q *CommandQueue) Dequeue() (Command, bool) {
	if len(q.commands) == 0 {
		return nil, false
	}
	command := q.commands[0]
	q.commands[0] = nil
	q.commands = q.commands[1:]
	if len(q.commands) == 0 {
		q.commands = nil
	}
	return command, true
}

func (q *CommandQueue) Len() int { return len(q.commands) }

// ErrorRule selects a response using both the failed command and its error.
// Rules are checked in registration order.
type ErrorRule struct {
	Match  func(Command, error) bool
	Handle func(Command, error) error
}

type Processor struct {
	Queue *CommandQueue
	Rules []ErrorRule
}

// RunNext processes one queued command. A handler may enqueue later work.
// An unhandled failure is returned to the caller instead of being discarded.
func (p *Processor) RunNext() (bool, error) {
	if p.Queue == nil {
		return false, errors.New("processor queue is nil")
	}
	command, ok := p.Queue.Dequeue()
	if !ok {
		return false, nil
	}
	err := executeSafely(command)
	if err == nil {
		return true, nil
	}
	for _, rule := range p.Rules {
		if rule.Match(command, err) {
			if handleErr := rule.Handle(command, err); handleErr != nil {
				return true, fmt.Errorf("handle %T failure: %w", command, handleErr)
			}
			return true, nil
		}
	}
	return true, fmt.Errorf("execute %T: %w", command, err)
}

// Run drains the queue, including commands appended by handlers.
func (p *Processor) Run() error {
	for {
		processed, err := p.RunNext()
		if err != nil || !processed {
			return err
		}
	}
}

// Recover is confined to the worker boundary. Normal Go failures use error.
func executeSafely(command Command) (err error) {
	defer func() {
		if value := recover(); value != nil {
			if panicErr, ok := value.(error); ok {
				err = fmt.Errorf("command panic: %w", panicErr)
			} else {
				err = fmt.Errorf("command panic: %v", value)
			}
		}
	}()
	return command.Execute()
}
