package spacebattle

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

type actionCommand func() error

func (command actionCommand) Execute() error { return command() }

type fuelStub struct {
	fuel     int
	cost     int
	fuelErr  error
	costErr  error
	writeErr error
	writes   int
}

func (state *fuelStub) Fuel() (int, error)            { return state.fuel, state.fuelErr }
func (state *fuelStub) FuelConsumption() (int, error) { return state.cost, state.costErr }
func (state *fuelStub) SetFuel(value int) error {
	state.writes++
	if state.writeErr != nil {
		return state.writeErr
	}
	state.fuel = value
	return nil
}

type fuelMover struct {
	*fuelStub
	position    Vector
	velocity    Vector
	positionErr error
}

func (mover *fuelMover) Position() (Vector, error) { return mover.position, nil }
func (mover *fuelMover) Velocity() (Vector, error) { return mover.velocity, nil }
func (mover *fuelMover) SetPosition(value Vector) error {
	if mover.positionErr != nil {
		return mover.positionErr
	}
	mover.position = value
	return nil
}

func TestCheckAndBurnFuel(t *testing.T) {
	state := &fuelStub{fuel: 5, cost: 3}
	if err := (CheckFuel{Object: state}).Execute(); err != nil {
		t.Fatal(err)
	}
	if state.fuel != 5 || state.writes != 0 {
		t.Fatal("check must not consume fuel")
	}
	if err := (BurnFuel{Object: state}).Execute(); err != nil {
		t.Fatal(err)
	}
	if state.fuel != 2 || state.writes != 1 {
		t.Fatalf("fuel=%d, writes=%d", state.fuel, state.writes)
	}
	if err := (CheckFuel{Object: state}).Execute(); !errors.Is(err, ErrInsufficientFuel) {
		t.Fatalf("insufficient fuel: %v", err)
	}
	if err := (BurnFuel{Object: state}).Execute(); !errors.Is(err, ErrInsufficientFuel) || state.writes != 1 {
		t.Fatalf("burn must not make fuel negative: %v, writes=%d", err, state.writes)
	}
}

func TestFuelCommandsPreserveAccessErrors(t *testing.T) {
	cause := errors.New("unavailable")
	for _, test := range []struct {
		name  string
		state fuelStub
		cmd   func(*fuelStub) Command
	}{
		{"read fuel", fuelStub{fuelErr: cause}, func(s *fuelStub) Command { return CheckFuel{Object: s} }},
		{"read cost", fuelStub{costErr: cause}, func(s *fuelStub) Command { return CheckFuel{Object: s} }},
		{"write fuel", fuelStub{fuel: 5, cost: 3, writeErr: cause}, func(s *fuelStub) Command { return BurnFuel{Object: s} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := test.state
			if err := test.cmd(&state).Execute(); !errors.Is(err, cause) {
				t.Fatalf("cause lost: %v", err)
			}
		})
	}
	if err := (CheckFuel{}).Execute(); !errors.Is(err, ErrNilObject) {
		t.Fatalf("nil object: %v", err)
	}
	if err := (BurnFuel{Object: &fuelStub{fuel: -1}}).Execute(); !errors.Is(err, ErrInvalidFuel) {
		t.Fatalf("negative fuel: %v", err)
	}
}

func TestMacroStopsAndWrapsFirstFailure(t *testing.T) {
	cause := errors.New("step failed")
	order := []int{}
	commands := []Command{
		actionCommand(func() error { order = append(order, 1); return nil }),
		actionCommand(func() error { order = append(order, 2); return cause }),
		actionCommand(func() error { order = append(order, 3); return nil }),
	}
	macro, err := NewMacroCommand(commands...)
	if err != nil {
		t.Fatal(err)
	}
	commands[0] = actionCommand(func() error { t.Fatal("caller changed command slice"); return nil })
	err = macro.Execute()
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != 2 || !errors.Is(err, cause) {
		t.Fatalf("macro error: %v", err)
	}
	if fmt.Sprint(order) != "[1 2]" {
		t.Fatalf("order: %v", order)
	}
}

func TestMacroConvertsNestedPanicToCommandError(t *testing.T) {
	cause := errors.New("panic cause")
	macro, err := NewMacroCommand(actionCommand(func() error { panic(cause) }))
	if err != nil {
		t.Fatal(err)
	}
	var commandErr *CommandError
	if err := macro.Execute(); !errors.As(err, &commandErr) || !errors.Is(err, cause) || commandErr.Step != 1 {
		t.Fatalf("panic result: %v", err)
	}
}

func TestMoveWithFuel(t *testing.T) {
	for _, test := range []struct {
		fuel     int
		wantFuel int
		wantPos  Vector
		wantErr  error
	}{
		{10, 7, Vector{X: 14, Y: 5}, nil},
		{3, 0, Vector{X: 14, Y: 5}, nil},
		{2, 2, Vector{X: 12, Y: 5}, ErrInsufficientFuel},
	} {
		direction, _ := NewDirection(0, 4)
		base, _ := NewShip(Vector{X: 12, Y: 5}, 2, direction, 1)
		ship, err := NewFueledShip(base, test.fuel, 3)
		if err != nil {
			t.Fatal(err)
		}
		macro, err := NewMoveWithFuel(ship)
		if err != nil {
			t.Fatal(err)
		}
		err = macro.Execute()
		if !errors.Is(err, test.wantErr) {
			t.Fatalf("fuel=%d, err=%v", test.fuel, err)
		}
		position, _ := ship.Position()
		fuel, _ := ship.Fuel()
		if position != test.wantPos || fuel != test.wantFuel {
			t.Fatalf("position=%+v, fuel=%d", position, fuel)
		}
	}
}

func TestMoveFailureSkipsFuelBurn(t *testing.T) {
	cause := errors.New("position rejected")
	state := &fuelStub{fuel: 10, cost: 3}
	mover := &fuelMover{fuelStub: state, position: Vector{X: 1}, velocity: Vector{X: 2}, positionErr: cause}
	macro, err := NewMoveWithFuel(mover)
	if err != nil {
		t.Fatal(err)
	}
	var commandErr *CommandError
	if err := macro.Execute(); !errors.Is(err, cause) || !errors.As(err, &commandErr) || commandErr.Step != 2 {
		t.Fatalf("move failure: %v", err)
	}
	if state.fuel != 10 || state.writes != 0 {
		t.Fatalf("burn ran after move failure: fuel=%d, writes=%d", state.fuel, state.writes)
	}
}

func TestBurnFailureDoesNotRollBackMovement(t *testing.T) {
	cause := errors.New("fuel store unavailable")
	state := &fuelStub{fuel: 10, cost: 3, writeErr: cause}
	mover := &fuelMover{fuelStub: state, position: Vector{X: 1}, velocity: Vector{X: 2}}
	macro, err := NewMoveWithFuel(mover)
	if err != nil {
		t.Fatal(err)
	}
	queue := &CommandQueue{}
	_ = queue.Enqueue(macro)
	processor := Processor{Queue: queue}
	var commandErr *CommandError
	if err := processor.Run(); !errors.Is(err, cause) || !errors.As(err, &commandErr) || commandErr.Step != 3 {
		t.Fatalf("burn failure: %v", err)
	}
	if mover.position != (Vector{X: 3}) || state.fuel != 10 {
		t.Fatalf("partial action: position=%+v, fuel=%d", mover.position, state.fuel)
	}
}

type turret struct {
	direction Direction
	steps     int
}

func (object *turret) Direction() (Direction, error) { return object.direction, nil }
func (object *turret) AngularVelocity() (int, error) { return object.steps, nil }
func (object *turret) SetDirection(direction Direction) error {
	object.direction = direction
	return nil
}

func TestRotateWithVelocityUpdatesStoredVector(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	base, _ := NewShip(Vector{}, 2, direction, 1)
	ship, err := NewFueledShip(base, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Rotate{Object: ship}).Execute(); err != nil {
		t.Fatal(err)
	}
	stale, _ := ship.Velocity()
	if stale != (Vector{X: 2}) {
		t.Fatalf("standalone rotate should not update cached vector: %+v", stale)
	}
	if err := (ChangeVelocity{Object: ship}).Execute(); err != nil {
		t.Fatal(err)
	}
	updated, _ := ship.Velocity()
	if math.Abs(updated.X) > 1e-12 || math.Abs(updated.Y-2) > 1e-12 {
		t.Fatalf("updated vector: %+v", updated)
	}
	// The composed command rotates again and updates the next movement.
	macro, err := NewRotateWithVelocity(ship)
	if err != nil {
		t.Fatal(err)
	}
	if err := macro.Execute(); err != nil {
		t.Fatal(err)
	}
	move, _ := NewMoveWithFuel(ship)
	if err := move.Execute(); err != nil {
		t.Fatal(err)
	}
	position, _ := ship.Position()
	fuel, _ := ship.Fuel()
	if math.Abs(position.X+2) > 1e-12 || math.Abs(position.Y) > 1e-12 || fuel != 4 {
		t.Fatalf("position=%+v, fuel=%d", position, fuel)
	}
}

func TestRotateWithVelocitySupportsNonmovingObject(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	object := &turret{direction: direction, steps: 1}
	macro, err := NewRotateWithVelocity(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := macro.Execute(); err != nil || object.direction.Index() != 1 {
		t.Fatalf("turret rotation: direction=%d, err=%v", object.direction.Index(), err)
	}
}

func TestExistingShipStillComputesVelocityFromDirection(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	ship, _ := NewShip(Vector{}, 2, direction, 1)
	macro, err := NewRotateWithVelocity(ship)
	if err != nil {
		t.Fatal(err)
	}
	if err := macro.Execute(); err != nil {
		t.Fatal(err)
	}
	velocity, _ := ship.Velocity()
	if math.Abs(velocity.X) > 1e-12 || math.Abs(velocity.Y-2) > 1e-12 {
		t.Fatalf("old ship changed behavior: %+v", velocity)
	}
}

func TestMacroAndFueledShipRejectInvalidConstruction(t *testing.T) {
	if _, err := NewMacroCommand(nil); !errors.Is(err, ErrNilCommand) {
		t.Fatalf("nil nested command: %v", err)
	}
	if _, err := NewFueledShip(nil, 1, 1); !errors.Is(err, ErrNilObject) {
		t.Fatalf("nil ship: %v", err)
	}
	direction, _ := NewDirection(0, 4)
	ship, _ := NewShip(Vector{}, 1, direction, 1)
	if _, err := NewFueledShip(ship, -1, 1); !errors.Is(err, ErrInvalidFuel) {
		t.Fatalf("negative fuel: %v", err)
	}
}
