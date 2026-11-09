package spacebattle

import (
	"errors"
	"math"
)

var ErrInvalidDirection = errors.New("direction requires a positive sector count")

// Direction is one of a fixed number of equally spaced headings.
// Its zero value is invalid; construct it with NewDirection.
type Direction struct {
	index   int
	sectors int
}

func NewDirection(index, sectors int) (Direction, error) {
	if sectors <= 0 {
		return Direction{}, ErrInvalidDirection
	}
	return Direction{index: normalize(index, sectors), sectors: sectors}, nil
}

func (d Direction) Index() int {
	return d.index
}

func (d Direction) Sectors() int {
	return d.sectors
}

func (d Direction) Next(steps int) (Direction, error) {
	if d.sectors <= 0 {
		return Direction{}, ErrInvalidDirection
	}
	current := normalize(d.index, d.sectors)
	step := normalize(steps, d.sectors)
	remaining := d.sectors - current
	if step >= remaining {
		return Direction{index: step - remaining, sectors: d.sectors}, nil
	}
	return Direction{index: current + step, sectors: d.sectors}, nil
}

func (d Direction) radians() (float64, error) {
	if d.sectors <= 0 {
		return 0, ErrInvalidDirection
	}
	return 2 * math.Pi * float64(d.index) / float64(d.sectors), nil
}

func normalize(value, sectors int) int {
	remainder := value % sectors
	if remainder < 0 {
		remainder += sectors
	}
	return remainder
}
