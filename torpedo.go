package spacebattle

// Torpedo has a fixed velocity vector and does not need to support rotation.
type Torpedo struct {
	position Vector
	velocity Vector
}

func NewTorpedo(position, velocity Vector) (*Torpedo, error) {
	if !position.valid() || !velocity.valid() {
		return nil, ErrInvalidVector
	}
	return &Torpedo{position: position, velocity: velocity}, nil
}

func (t *Torpedo) Position() (Vector, error) {
	return t.position, nil
}

func (t *Torpedo) Velocity() (Vector, error) {
	return t.velocity, nil
}

func (t *Torpedo) SetPosition(position Vector) error {
	if !position.valid() {
		return ErrInvalidVector
	}
	t.position = position
	return nil
}

var _ MovingObject = (*Torpedo)(nil)
