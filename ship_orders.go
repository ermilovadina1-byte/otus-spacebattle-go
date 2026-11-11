package spacebattle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidStartSpeed = errors.New("start speed must be finite and positive")

// SpeedControllable is the capability needed by movement orders.
type SpeedControllable interface {
	SetSpeed(float64) error
}

// Fireable is the capability needed by a shoot order. The weapon decides
// whether firing creates a projectile, consumes ammunition, or has no effect.
type Fireable interface {
	Fire() error
}

type StartMove struct {
	Object SpeedControllable
	Speed  float64
}

func (command StartMove) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	if !finite(command.Speed) || command.Speed <= 0 {
		return ErrInvalidStartSpeed
	}
	return command.Object.SetSpeed(command.Speed)
}

type StopMove struct{ Object SpeedControllable }

func (command StopMove) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	return command.Object.SetSpeed(0)
}

type Shoot struct{ Object Fireable }

func (command Shoot) Execute() error {
	if command.Object == nil {
		return ErrNilObject
	}
	return command.Object.Fire()
}

// RegisterShipOrderOperations installs three trusted IoC factories and returns
// the public operation IDs for GameConfig. Call it during game assembly, before
// the worker starts. An error leaves any earlier registrations in place.
func RegisterShipOrderOperations(ioc *IoC, session *ScopeSession) (map[string]string, error) {
	entries := []struct {
		operation string
		key       string
		factory   Factory
	}{
		{"StartMove", "game.order.start-move", startMoveOrderFactory},
		{"StopMove", "game.order.stop-move", stopMoveOrderFactory},
		{"Shoot", "game.order.shoot", shootOrderFactory},
	}
	operations := make(map[string]string, len(entries))
	for _, entry := range entries {
		registration, err := ioc.Resolve[Command](session, "IoC.Register", entry.key, entry.factory)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", entry.operation, err)
		}
		if err := registration.Execute(); err != nil {
			return nil, fmt.Errorf("register %s: %w", entry.operation, err)
		}
		operations[entry.operation] = entry.key
	}
	return operations, nil
}

func startMoveOrderFactory(args ...any) (any, error) {
	if len(args) != 2 {
		return nil, ErrInvalidArguments
	}
	object, ok := args[0].(SpeedControllable)
	if !ok || object == nil {
		return nil, ErrInvalidArguments
	}
	var payload struct {
		InitialVelocity *float64 `json:"initialVelocity"`
	}
	if err := decodeOrderPayload(args[1], &payload); err != nil {
		return nil, err
	}
	if payload.InitialVelocity == nil || !finite(*payload.InitialVelocity) || *payload.InitialVelocity <= 0 {
		return nil, ErrInvalidStartSpeed
	}
	return StartMove{Object: object, Speed: *payload.InitialVelocity}, nil
}

func stopMoveOrderFactory(args ...any) (any, error) {
	if len(args) != 2 {
		return nil, ErrInvalidArguments
	}
	object, ok := args[0].(SpeedControllable)
	if !ok || object == nil {
		return nil, ErrInvalidArguments
	}
	var payload struct{}
	if err := decodeOrderPayload(args[1], &payload); err != nil {
		return nil, err
	}
	return StopMove{Object: object}, nil
}

func shootOrderFactory(args ...any) (any, error) {
	if len(args) != 2 {
		return nil, ErrInvalidArguments
	}
	object, ok := args[0].(Fireable)
	if !ok || object == nil {
		return nil, ErrInvalidArguments
	}
	var payload struct{}
	if err := decodeOrderPayload(args[1], &payload); err != nil {
		return nil, err
	}
	return Shoot{Object: object}, nil
}

func decodeOrderPayload(value any, destination any) error {
	raw, ok := value.(json.RawMessage)
	if !ok {
		return ErrInvalidArguments
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ErrInvalidArguments
	}
	return nil
}

var _ SpeedControllable = (*Ship)(nil)
var _ Command = StartMove{}
var _ Command = StopMove{}
var _ Command = Shoot{}
