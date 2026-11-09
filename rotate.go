package spacebattle

import "fmt"

// RotatingObject exposes only the state needed for discrete rotation.
type RotatingObject interface {
	Direction() (Direction, error)
	AngularVelocity() (int, error)
	SetDirection(Direction) error
}

// Rotate turns an object by its angular velocity for one game step.
type Rotate struct {
	Object RotatingObject
}

func (command Rotate) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	direction, err := command.Object.Direction()
	if err != nil {
		return fmt.Errorf("read direction: %w", err)
	}
	steps, err := command.Object.AngularVelocity()
	if err != nil {
		return fmt.Errorf("read angular velocity: %w", err)
	}
	next, err := direction.Next(steps)
	if err != nil {
		return fmt.Errorf("compute next direction: %w", err)
	}
	if err := command.Object.SetDirection(next); err != nil {
		return fmt.Errorf("write direction: %w", err)
	}
	return nil
}
