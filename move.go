package spacebattle

import (
	"errors"
	"fmt"
)

var ErrNilObject = errors.New("game object is nil")

// MovingObject exposes only the state needed for straight uniform movement.
type MovingObject interface {
	Position() (Vector, error)
	Velocity() (Vector, error)
	SetPosition(Vector) error
}

// Move advances an object by one game step.
type Move struct {
	Object MovingObject
}

func (command Move) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	position, err := command.Object.Position()
	if err != nil {
		return fmt.Errorf("read position: %w", err)
	}
	if !position.valid() {
		return fmt.Errorf("read position: %w", ErrInvalidVector)
	}
	velocity, err := command.Object.Velocity()
	if err != nil {
		return fmt.Errorf("read velocity: %w", err)
	}
	if !velocity.valid() {
		return fmt.Errorf("read velocity: %w", ErrInvalidVector)
	}
	next := position.Add(velocity)
	if !next.valid() {
		return fmt.Errorf("compute next position: %w", ErrInvalidVector)
	}
	if err := command.Object.SetPosition(next); err != nil {
		return fmt.Errorf("write position: %w", err)
	}
	return nil
}
