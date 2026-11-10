package spacebattle

import "errors"

var (
	ErrMoveToTarget         = errors.New("move-to target must be another command worker")
	ErrModeCommandNotQueued = errors.New("mode command must run inside a command worker")
)

// MoveToCommand switches the current worker to forwarding subsequent commands
// into Target's intake queue. Target may be started later.
type MoveToCommand struct{ Target *CommandWorker }

func (MoveToCommand) Execute() error { return ErrModeCommandNotQueued }

// RunCommand switches a forwarding worker back to local execution.
type RunCommand struct{}

func (RunCommand) Execute() error { return ErrModeCommandNotQueued }

// A state handles exactly one dequeued command and chooses the state for the
// next command. Returning nil ends the worker and discards pending work.
type workerState interface {
	handle(*CommandWorker, Command) (workerState, error)
}

type runningState struct{}

func (s *runningState) handle(w *CommandWorker, command Command) (workerState, error) {
	if target, ok := moveToTarget(command); ok {
		if target == nil || target == w {
			return s, ErrMoveToTarget
		}
		return &moveToState{target: target, running: s}, nil
	}
	if isRunCommand(command) {
		return s, nil
	}
	if isSelfHardStop(w, command) {
		return nil, nil
	}
	return s, executeSafely(command)
}

type moveToState struct {
	target  *CommandWorker
	running *runningState
}

func (s *moveToState) handle(w *CommandWorker, command Command) (workerState, error) {
	if target, ok := moveToTarget(command); ok {
		if target == nil || target == w {
			return s, ErrMoveToTarget
		}
		return &moveToState{target: target, running: s.running}, nil
	}
	if isRunCommand(command) {
		return s.running, nil
	}
	if isSelfHardStop(w, command) {
		return nil, nil
	}
	if isSelfSoftStop(w, command) {
		return s, executeSafely(command)
	}
	return s, s.target.Enqueue(command)
}

func moveToTarget(command Command) (*CommandWorker, bool) {
	switch value := command.(type) {
	case MoveToCommand:
		return value.Target, true
	case *MoveToCommand:
		if value == nil {
			return nil, true
		}
		return value.Target, true
	default:
		return nil, false
	}
}

func isRunCommand(command Command) bool {
	switch command.(type) {
	case RunCommand, *RunCommand:
		return true
	default:
		return false
	}
}

func isSelfHardStop(w *CommandWorker, command Command) bool {
	switch value := command.(type) {
	case HardStopWorker:
		return value.Worker == w
	case *HardStopWorker:
		return value != nil && value.Worker == w
	default:
		return false
	}
}

func isSelfSoftStop(w *CommandWorker, command Command) bool {
	switch value := command.(type) {
	case SoftStopWorker:
		return value.Worker == w
	case *SoftStopWorker:
		return value != nil && value.Worker == w
	default:
		return false
	}
}

var _ Command = MoveToCommand{}
var _ Command = RunCommand{}
var _ workerState = (*runningState)(nil)
var _ workerState = (*moveToState)(nil)
