package spacebattle

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type adapterTestObject struct {
	mu     sync.RWMutex
	values map[string]any
}

func (object *adapterTestObject) get(name string) (any, error) {
	object.mu.RLock()
	defer object.mu.RUnlock()
	value, exists := object.values[name]
	if !exists {
		return nil, ErrUnknownDependency
	}
	return value, nil
}

func (object *adapterTestObject) set(name string, value any) error {
	object.mu.Lock()
	object.values[name] = value
	object.mu.Unlock()
	return nil
}

type adapterTestCommand func() error

func (command adapterTestCommand) Execute() error { return command() }

func registerAdapterGetter(t *testing.T, ioc *IoC, session *ScopeSession, key, property string) {
	t.Helper()
	factory := Factory(func(args ...any) (any, error) {
		if len(args) != 1 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(*adapterTestObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		return object.get(property)
	})
	if err := mustResolveCommand(t, ioc, session, "IoC.Register", key, factory).Execute(); err != nil {
		t.Fatal(err)
	}
}

func registerAdapterSetter(t *testing.T, ioc *IoC, key, property string) {
	t.Helper()
	factory := Factory(func(args ...any) (any, error) {
		if len(args) != 2 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(*adapterTestObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		return adapterTestCommand(func() error { return object.set(property, args[1]) }), nil
	})
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", key, factory).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedAdaptersRunExistingCommands(t *testing.T) {
	ioc := NewIoC()
	registry := NewAdapterRegistry()
	for _, register := range []func(*AdapterRegistry) error{
		RegisterMovingObjectAdapter,
		RegisterRotatingObjectAdapter,
		RegisterFinishingObjectAdapter,
	} {
		if err := register(registry); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Install(ioc, nil); err != nil {
		t.Fatal(err)
	}
	for key, property := range map[string]string{
		"MovingObject:position.get":          "position",
		"MovingObject:velocity.get":          "velocity",
		"RotatingObject:direction.get":       "direction",
		"RotatingObject:angularVelocity.get": "angularVelocity",
	} {
		registerAdapterGetter(t, ioc, nil, key, property)
	}
	registerAdapterSetter(t, ioc, "MovingObject:position.set", "position")
	registerAdapterSetter(t, ioc, "RotatingObject:direction.set", "direction")
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "FinishingObject:finish.call", Factory(func(args ...any) (any, error) {
		if len(args) != 1 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(*adapterTestObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		return adapterTestCommand(func() error { return object.set("finished", true) }), nil
	})).Execute(); err != nil {
		t.Fatal(err)
	}

	direction, _ := NewDirection(1, 8)
	object := &adapterTestObject{values: map[string]any{
		"position": Vector{X: 3, Y: 4}, "velocity": Vector{X: 2, Y: -1},
		"direction": direction, "angularVelocity": 2,
	}}
	moving, err := ioc.Resolve[MovingObject](nil, "Adapter", reflect.TypeFor[MovingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Move{Object: moving}).Execute(); err != nil {
		t.Fatal(err)
	}
	position, _ := object.get("position")
	if position != (Vector{X: 5, Y: 3}) {
		t.Fatalf("generated movement adapter did not update object: %v", position)
	}
	rotating, err := ioc.Resolve[RotatingObject](nil, "Adapter", reflect.TypeFor[RotatingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Rotate{Object: rotating}).Execute(); err != nil {
		t.Fatal(err)
	}
	changedDirection, _ := object.get("direction")
	if changedDirection.(Direction).Index() != 3 {
		t.Fatalf("generated rotation adapter did not update object: %v", changedDirection)
	}
	finishing, err := ioc.Resolve[FinishingObject](nil, "Adapter", reflect.TypeFor[FinishingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if err := finishing.Finish(); err != nil {
		t.Fatal(err)
	}
	finished, _ := object.get("finished")
	if finished != true {
		t.Fatalf("generated arbitrary method did not execute command: %v", finished)
	}
}

func TestAdapterUsesCallingScope(t *testing.T) {
	ioc := NewIoC()
	registry := NewAdapterRegistry()
	if err := RegisterMovingObjectAdapter(registry); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(ioc, nil); err != nil {
		t.Fatal(err)
	}
	registerAdapterGetter(t, ioc, nil, "MovingObject:position.get", "position")
	registerAdapterGetter(t, ioc, nil, "MovingObject:velocity.get", "velocity")
	registerAdapterSetter(t, ioc, "MovingObject:position.set", "position")
	child, _ := NewScopeSession(ioc)
	if err := mustResolveCommand(t, ioc, child, "Scopes.New", "fast-game").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := mustResolveCommand(t, ioc, child, "Scopes.Current", "fast-game").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := mustResolveCommand(t, ioc, child, "IoC.Register", "MovingObject:velocity.get", Factory(func(args ...any) (any, error) {
		return Vector{X: 10}, nil
	})).Execute(); err != nil {
		t.Fatal(err)
	}
	object := &adapterTestObject{values: map[string]any{"position": Vector{}, "velocity": Vector{X: 1}}}
	adapted, err := ioc.Resolve[MovingObject](child, "Adapter", reflect.TypeFor[MovingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Move{Object: adapted}).Execute(); err != nil {
		t.Fatal(err)
	}
	position, _ := object.get("position")
	if position != (Vector{X: 10}) {
		t.Fatalf("adapter lost caller's scope: %v", position)
	}
}

func TestAdapterRegistrationAndResolutionErrors(t *testing.T) {
	ioc := NewIoC()
	registry := NewAdapterRegistry()
	if err := registry.Register(reflect.TypeFor[int](), nil); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("non-interface registration: %v", err)
	}
	if err := RegisterMovingObjectAdapter(registry); err != nil {
		t.Fatal(err)
	}
	if err := RegisterMovingObjectAdapter(registry); !errors.Is(err, ErrDuplicateAdapterType) {
		t.Fatalf("duplicate interface: %v", err)
	}
	if err := registry.Install(ioc, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(ioc, nil); !errors.Is(err, ErrDuplicateDependency) {
		t.Fatalf("duplicate adapter strategy: %v", err)
	}
	object := &adapterTestObject{values: map[string]any{}}
	for _, args := range [][]any{
		nil,
		{reflect.TypeFor[MovingObject]()},
		{reflect.TypeFor[MovingObject](), nil},
		{"MovingObject", object},
	} {
		if _, err := ioc.Resolve[MovingObject](nil, "Adapter", args...); !errors.Is(err, ErrInvalidArguments) {
			t.Fatalf("invalid adapter arguments %v: %v", args, err)
		}
	}
	if _, err := ioc.Resolve[RotatingObject](nil, "Adapter", reflect.TypeFor[RotatingObject](), object); !errors.Is(err, ErrUnknownAdapterType) {
		t.Fatalf("unknown adapter type: %v", err)
	}
	adapted, err := ioc.Resolve[MovingObject](nil, "Adapter", reflect.TypeFor[MovingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapted.Position(); !errors.Is(err, ErrUnknownDependency) {
		t.Fatalf("missing property strategy: %v", err)
	}
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "MovingObject:position.get", Factory(func(...any) (any, error) {
		return 7, nil
	})).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := adapted.Position(); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("wrong property type: %v", err)
	}
	if err := RegisterFinishingObjectAdapter(registry); err != nil {
		t.Fatal(err)
	}
	if _, err := ioc.Resolve[FinishingObject](nil, "Adapter", reflect.TypeFor[FinishingObject](), object); !errors.Is(err, ErrUnknownAdapterType) {
		t.Fatalf("installed registry snapshot changed: %v", err)
	}
}

func TestGeneratedMethodPropagatesCommandError(t *testing.T) {
	ioc := NewIoC()
	registry := NewAdapterRegistry()
	if err := RegisterFinishingObjectAdapter(registry); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(ioc, nil); err != nil {
		t.Fatal(err)
	}
	want := errors.New("finish failed")
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "FinishingObject:finish.call", Factory(func(...any) (any, error) {
		return adapterTestCommand(func() error { return want }), nil
	})).Execute(); err != nil {
		t.Fatal(err)
	}
	object := &adapterTestObject{values: map[string]any{}}
	adapted, err := ioc.Resolve[FinishingObject](nil, "Adapter", reflect.TypeFor[FinishingObject](), object)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapted.Finish(); !errors.Is(err, want) {
		t.Fatalf("command error was lost: %v", err)
	}
}

func TestGeneratedAdapterSeparatesConcurrentScopes(t *testing.T) {
	ioc := NewIoC()
	registry := NewAdapterRegistry()
	if err := RegisterMovingObjectAdapter(registry); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(ioc, nil); err != nil {
		t.Fatal(err)
	}
	registerAdapterGetter(t, ioc, nil, "MovingObject:position.get", "position")
	registerAdapterSetter(t, ioc, "MovingObject:position.set", "position")

	const games = 16
	var group sync.WaitGroup
	results := make(chan error, games)
	for number := 1; number <= games; number++ {
		group.Add(1)
		go func() {
			defer group.Done()
			session, err := NewScopeSession(ioc)
			if err != nil {
				results <- err
				return
			}
			name := fmt.Sprintf("game-%d", number)
			create, err := ioc.Resolve[Command](session, "Scopes.New", name)
			if err == nil {
				err = create.Execute()
			}
			if err != nil {
				results <- err
				return
			}
			switchScope, err := ioc.Resolve[Command](session, "Scopes.Current", name)
			if err == nil {
				err = switchScope.Execute()
			}
			if err != nil {
				results <- err
				return
			}
			factory := Factory(func(args ...any) (any, error) { return Vector{X: float64(number)}, nil })
			register, err := ioc.Resolve[Command](session, "IoC.Register", "MovingObject:velocity.get", factory)
			if err == nil {
				err = register.Execute()
			}
			if err != nil {
				results <- err
				return
			}
			object := &adapterTestObject{values: map[string]any{"position": Vector{}}}
			adapted, err := ioc.Resolve[MovingObject](session, "Adapter", reflect.TypeFor[MovingObject](), object)
			if err == nil {
				err = (Move{Object: adapted}).Execute()
			}
			if err == nil {
				var position any
				position, err = object.get("position")
				if err == nil && position != (Vector{X: float64(number)}) {
					err = fmt.Errorf("game %d got position %v", number, position)
				}
			}
			results <- err
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}
