package spacebattle

import (
	"errors"
	"math"
)

var ErrInvalidVector = errors.New("vector must have finite coordinates")

// Vector is a position or a displacement in the game plane.
type Vector struct {
	X float64
	Y float64
}

func (v Vector) Add(other Vector) Vector {
	return Vector{X: v.X + other.X, Y: v.Y + other.Y}
}

func (v Vector) valid() bool {
	return finite(v.X) && finite(v.Y)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
