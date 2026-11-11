package spacebattle

import (
	"errors"
	"fmt"
	"math"
)

var ErrInvalidSpeed = errors.New("speed must be finite and non-negative")
var ErrIncompatibleDirection = errors.New("direction sector count differs from ship")

// Ship stores scalar speed and a discrete heading.
type Ship struct {
	position        Vector
	speed           float64
	direction       Direction
	angularVelocity int
}

func NewShip(position Vector, speed float64, direction Direction, angularVelocity int) (*Ship, error) {
	if !position.valid() {
		return nil, ErrInvalidVector
	}
	if !finite(speed) || speed < 0 {
		return nil, ErrInvalidSpeed
	}
	if direction.sectors <= 0 {
		return nil, ErrInvalidDirection
	}
	return &Ship{position: position, speed: speed, direction: direction, angularVelocity: angularVelocity}, nil
}

func (s *Ship) Position() (Vector, error) {
	return s.position, nil
}

func (s *Ship) Velocity() (Vector, error) {
	radians, err := s.direction.radians()
	if err != nil {
		return Vector{}, fmt.Errorf("ship heading: %w", err)
	}
	return Vector{X: s.speed * math.Cos(radians), Y: s.speed * math.Sin(radians)}, nil
}

func (s *Ship) SetPosition(position Vector) error {
	if !position.valid() {
		return ErrInvalidVector
	}
	s.position = position
	return nil
}

// SetSpeed changes the scalar speed used by subsequent Move commands.
// Zero stops movement without changing the ship's heading.
func (s *Ship) SetSpeed(speed float64) error {
	if s == nil {
		return ErrNilObject
	}
	if !finite(speed) || speed < 0 {
		return ErrInvalidSpeed
	}
	s.speed = speed
	return nil
}

func (s *Ship) Direction() (Direction, error) {
	return s.direction, nil
}

func (s *Ship) AngularVelocity() (int, error) {
	return s.angularVelocity, nil
}

func (s *Ship) SetDirection(direction Direction) error {
	if direction.sectors <= 0 {
		return ErrInvalidDirection
	}
	if direction.sectors != s.direction.sectors {
		return ErrIncompatibleDirection
	}
	s.direction = direction
	return nil
}

var _ MovingObject = (*Ship)(nil)
var _ RotatingObject = (*Ship)(nil)
