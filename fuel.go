package spacebattle

import (
	"errors"
	"fmt"
)

var ErrInvalidFuel = errors.New("fuel and consumption must be non-negative")
var ErrInsufficientFuel = errors.New("insufficient fuel")

// FuelState is the state required to check and consume fuel for one move.
type FuelState interface {
	Fuel() (int, error)
	FuelConsumption() (int, error)
	SetFuel(int) error
}

type CheckFuel struct{ Object FuelState }

func (command CheckFuel) Execute() error {
	fuel, cost, err := readFuel(command.Object)
	if err != nil {
		return err
	}
	if fuel < cost {
		return ErrInsufficientFuel
	}
	return nil
}

type BurnFuel struct{ Object FuelState }

func (command BurnFuel) Execute() error {
	fuel, cost, err := readFuel(command.Object)
	if err != nil {
		return err
	}
	if fuel < cost {
		return ErrInsufficientFuel
	}
	if err := command.Object.SetFuel(fuel - cost); err != nil {
		return fmt.Errorf("write fuel: %w", err)
	}
	return nil
}

func readFuel(object FuelState) (fuel, cost int, err error) {
	if object == nil {
		return 0, 0, ErrNilObject
	}
	fuel, err = object.Fuel()
	if err != nil {
		return 0, 0, fmt.Errorf("read fuel: %w", err)
	}
	cost, err = object.FuelConsumption()
	if err != nil {
		return 0, 0, fmt.Errorf("read fuel consumption: %w", err)
	}
	if fuel < 0 || cost < 0 {
		return 0, 0, ErrInvalidFuel
	}
	return fuel, cost, nil
}

var _ Command = CheckFuel{}
var _ Command = BurnFuel{}
