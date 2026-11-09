package spacebattle

import "fmt"

// VelocityAdjustable is implemented only by rotating objects with a stored
// velocity vector that must be updated after their direction changes.
type VelocityAdjustable interface {
	TargetVelocity() (Vector, error)
	SetVelocity(Vector) error
}

// ChangeVelocity is a no-op for rotating objects without stored velocity.
type ChangeVelocity struct{ Object RotatingObject }

func (command ChangeVelocity) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	object, ok := command.Object.(VelocityAdjustable)
	if !ok {
		return nil
	}
	velocity, err := object.TargetVelocity()
	if err != nil {
		return fmt.Errorf("compute velocity: %w", err)
	}
	if !velocity.valid() {
		return ErrInvalidVector
	}
	if err := object.SetVelocity(velocity); err != nil {
		return fmt.Errorf("write velocity: %w", err)
	}
	return nil
}

var _ Command = ChangeVelocity{}
