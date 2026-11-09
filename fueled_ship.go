package spacebattle

// FueledShip reuses Ship's movement rules but stores its velocity vector.
// Construction copies the source ship so later changes through that pointer
// cannot leave this object's direction and cached velocity inconsistent.
type FueledShip struct {
	base        Ship
	velocity    Vector
	fuel        int
	consumption int
}

func NewFueledShip(ship *Ship, fuel, consumption int) (*FueledShip, error) {
	if ship == nil {
		return nil, ErrNilObject
	}
	if fuel < 0 || consumption < 0 {
		return nil, ErrInvalidFuel
	}
	velocity, err := ship.Velocity()
	if err != nil {
		return nil, err
	}
	if !velocity.valid() {
		return nil, ErrInvalidVector
	}
	return &FueledShip{base: *ship, velocity: velocity, fuel: fuel, consumption: consumption}, nil
}

func (ship *FueledShip) Position() (Vector, error) { return ship.base.Position() }
func (ship *FueledShip) SetPosition(position Vector) error {
	return ship.base.SetPosition(position)
}
func (ship *FueledShip) Velocity() (Vector, error) { return ship.velocity, nil }
func (ship *FueledShip) Direction() (Direction, error) {
	return ship.base.Direction()
}
func (ship *FueledShip) AngularVelocity() (int, error) {
	return ship.base.AngularVelocity()
}
func (ship *FueledShip) SetDirection(direction Direction) error {
	return ship.base.SetDirection(direction)
}
func (ship *FueledShip) TargetVelocity() (Vector, error) {
	return ship.base.Velocity()
}
func (ship *FueledShip) SetVelocity(velocity Vector) error {
	if !velocity.valid() {
		return ErrInvalidVector
	}
	ship.velocity = velocity
	return nil
}
func (ship *FueledShip) Fuel() (int, error) { return ship.fuel, nil }
func (ship *FueledShip) FuelConsumption() (int, error) {
	return ship.consumption, nil
}
func (ship *FueledShip) SetFuel(fuel int) error {
	if fuel < 0 {
		return ErrInvalidFuel
	}
	ship.fuel = fuel
	return nil
}

var _ FuelMovingObject = (*FueledShip)(nil)
var _ RotatingObject = (*FueledShip)(nil)
var _ VelocityAdjustable = (*FueledShip)(nil)
