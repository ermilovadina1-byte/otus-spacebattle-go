package spacebattle

import "fmt"

// CommandError identifies the failed one-based step and preserves its cause.
type CommandError struct {
	Step  int
	Cause error
}

func (err *CommandError) Error() string {
	return fmt.Sprintf("command step %d: %v", err.Step, err.Cause)
}

func (err *CommandError) Unwrap() error { return err.Cause }

// MacroCommand runs its commands in order and stops at the first failure.
type MacroCommand struct{ commands []Command }

func NewMacroCommand(commands ...Command) (*MacroCommand, error) {
	for _, command := range commands {
		if command == nil {
			return nil, ErrNilCommand
		}
	}
	return &MacroCommand{commands: append([]Command(nil), commands...)}, nil
}

func (macro *MacroCommand) Execute() error {
	for index, command := range macro.commands {
		if err := executeSafely(command); err != nil {
			return &CommandError{Step: index + 1, Cause: err}
		}
	}
	return nil
}

// FuelMovingObject can move and expose its fuel state to the two fuel commands.
type FuelMovingObject interface {
	MovingObject
	FuelState
}

func NewMoveWithFuel(object FuelMovingObject) (*MacroCommand, error) {
	if object == nil {
		return nil, ErrNilObject
	}
	return NewMacroCommand(CheckFuel{Object: object}, Move{Object: object}, BurnFuel{Object: object})
}

func NewRotateWithVelocity(object RotatingObject) (*MacroCommand, error) {
	if object == nil {
		return nil, ErrNilObject
	}
	return NewMacroCommand(Rotate{Object: object}, ChangeVelocity{Object: object})
}

var _ Command = (*MacroCommand)(nil)
