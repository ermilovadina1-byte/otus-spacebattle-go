package spacebattle

import "errors"

var ErrNilWeapon = errors.New("weapon callback is nil")

// ArmedShip keeps the existing Ship movement rules and delegates firing to a
// weapon callback. Construction copies the ship so outside mutations cannot
// change the game object's state behind its worker.
type ArmedShip struct {
	Ship
	fire func() error
}

func NewArmedShip(ship *Ship, fire func() error) (*ArmedShip, error) {
	if ship == nil {
		return nil, ErrNilObject
	}
	if fire == nil {
		return nil, ErrNilWeapon
	}
	return &ArmedShip{Ship: *ship, fire: fire}, nil
}

func (ship *ArmedShip) Fire() error {
	if ship == nil {
		return ErrNilObject
	}
	if ship.fire == nil {
		return ErrNilWeapon
	}
	return ship.fire()
}

var _ MovingObject = (*ArmedShip)(nil)
var _ RotatingObject = (*ArmedShip)(nil)
var _ SpeedControllable = (*ArmedShip)(nil)
var _ Fireable = (*ArmedShip)(nil)
