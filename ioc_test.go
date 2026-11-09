package spacebattle

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustResolveCommand(t *testing.T, ioc *IoC, session *ScopeSession, key string, args ...any) Command {
	t.Helper()
	command, err := ioc.Resolve[Command](session, key, args...)
	if err != nil {
		t.Fatalf("resolve %s: %v", key, err)
	}
	return command
}

func TestIoCRegisterAndResolveThroughOneMethod(t *testing.T) {
	ioc := NewIoC()
	register := mustResolveCommand(t, ioc, nil, "IoC.Register", "answer", Factory(func(args ...any) (any, error) {
		if len(args) != 1 || args[0] != 40 {
			return nil, ErrInvalidArguments
		}
		return 42, nil
	}))
	if _, err := ioc.Resolve[int](nil, "answer", 40); !errors.Is(err, ErrUnknownDependency) {
		t.Fatalf("registration ran before Execute: %v", err)
	}
	if err := register.Execute(); err != nil {
		t.Fatal(err)
	}
	answer, err := ioc.Resolve[int](nil, "answer", 40)
	if err != nil || answer != 42 {
		t.Fatalf("answer=%d, err=%v", answer, err)
	}
	if _, err := ioc.Resolve[string](nil, "answer", 40); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("wrong result type: %v", err)
	}
	if _, err := ioc.Resolve[int](nil, "answer", 41); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("factory error lost: %v", err)
	}
}

func TestIoCRejectsInvalidConfiguration(t *testing.T) {
	ioc := NewIoC()
	for _, test := range []struct {
		key  string
		args []any
		want error
	}{
		{"", nil, ErrInvalidArguments},
		{"IoC.Register", []any{"x"}, ErrInvalidArguments},
		{"IoC.Register", []any{"x", 42}, ErrInvalidArguments},
		{"IoC.Register", []any{"Scopes.New", Factory(func(...any) (any, error) { return 1, nil })}, ErrReservedKey},
		{"Scopes.New", []any{"root"}, ErrReservedKey},
		{"Scopes.New", []any{0}, ErrInvalidArguments},
		{"Scopes.Current", []any{"root"}, ErrInvalidArguments},
	} {
		if _, err := ioc.Resolve[Command](nil, test.key, test.args...); !errors.Is(err, test.want) {
			t.Fatalf("key=%q, args=%v: %v", test.key, test.args, err)
		}
	}
	if _, err := (*IoC)(nil).Resolve[int](nil, "x"); !errors.Is(err, ErrNilIoC) {
		t.Fatalf("nil IoC: %v", err)
	}
	other := NewIoC()
	foreign, _ := NewScopeSession(other)
	if _, err := ioc.Resolve[int](foreign, "x"); !errors.Is(err, ErrInvalidScopeSession) {
		t.Fatalf("foreign session: %v", err)
	}
}

func TestScopeInheritanceAndOverrides(t *testing.T) {
	ioc := NewIoC()
	rootFactory := Factory(func(...any) (any, error) { return "root", nil })
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "mode", rootFactory).Execute(); err != nil {
		t.Fatal(err)
	}
	session, _ := NewScopeSession(ioc)
	create := mustResolveCommand(t, ioc, session, "Scopes.New", "game-1")
	if _, err := ioc.Resolve[Command](session, "Scopes.Current", "game-1"); err != nil {
		t.Fatalf("command construction should not switch scope: %v", err)
	}
	if err := create.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.Current", "game-1").Execute(); err != nil {
		t.Fatal(err)
	}
	mode, err := ioc.Resolve[string](session, "mode")
	if err != nil || mode != "root" {
		t.Fatalf("parent lookup: mode=%q, err=%v", mode, err)
	}
	if err := mustResolveCommand(t, ioc, session, "IoC.Register", "mode", Factory(func(...any) (any, error) { return "game-1", nil })).Execute(); err != nil {
		t.Fatal(err)
	}
	mode, err = ioc.Resolve[string](session, "mode")
	if err != nil || mode != "game-1" {
		t.Fatalf("scope override: mode=%q, err=%v", mode, err)
	}
	rootMode, _ := ioc.Resolve[string](nil, "mode")
	if rootMode != "root" {
		t.Fatalf("root was changed: %q", rootMode)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.New", "nested").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.Current", "nested").Execute(); err != nil {
		t.Fatal(err)
	}
	mode, err = ioc.Resolve[string](session, "mode")
	if err != nil || mode != "game-1" {
		t.Fatalf("nested parent lookup: mode=%q, err=%v", mode, err)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.Current", "root").Execute(); err != nil {
		t.Fatal(err)
	}
	mode, _ = ioc.Resolve[string](session, "mode")
	if mode != "root" {
		t.Fatalf("switch back to root: %q", mode)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.New", "game-1").Execute(); !errors.Is(err, ErrDuplicateScope) {
		t.Fatalf("duplicate scope: %v", err)
	}
	if err := mustResolveCommand(t, ioc, session, "Scopes.Current", "missing").Execute(); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("unknown scope: %v", err)
	}
	if err := mustResolveCommand(t, ioc, session, "IoC.Register", "mode", rootFactory).Execute(); !errors.Is(err, ErrDuplicateDependency) {
		t.Fatalf("duplicate registration: %v", err)
	}
}

func TestIoCChoosesExistingGameCommandsByScope(t *testing.T) {
	ioc := NewIoC()
	plainMove := Factory(func(args ...any) (any, error) {
		if len(args) != 1 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(MovingObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		return Move{Object: object}, nil
	})
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "game.move", plainMove).Execute(); err != nil {
		t.Fatal(err)
	}
	fuelSession, _ := NewScopeSession(ioc)
	if err := mustResolveCommand(t, ioc, fuelSession, "Scopes.New", "fuel-game").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := mustResolveCommand(t, ioc, fuelSession, "Scopes.Current", "fuel-game").Execute(); err != nil {
		t.Fatal(err)
	}
	fuelMove := Factory(func(args ...any) (any, error) {
		if len(args) != 1 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(FuelMovingObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		return NewMoveWithFuel(object)
	})
	if err := mustResolveCommand(t, ioc, fuelSession, "IoC.Register", "game.move", fuelMove).Execute(); err != nil {
		t.Fatal(err)
	}
	torpedo, _ := NewTorpedo(Vector{X: 1}, Vector{X: 2})
	plain, err := ioc.Resolve[Command](nil, "game.move", torpedo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.(Move); !ok {
		t.Fatalf("root should create Move, got %T", plain)
	}
	if err := plain.Execute(); err != nil {
		t.Fatal(err)
	}
	position, _ := torpedo.Position()
	if position != (Vector{X: 3}) {
		t.Fatalf("plain move: %+v", position)
	}
	direction, _ := NewDirection(0, 4)
	base, _ := NewShip(Vector{X: 1}, 2, direction, 1)
	ship, _ := NewFueledShip(base, 5, 1)
	fueled, err := ioc.Resolve[Command](fuelSession, "game.move", ship)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fueled.(*MacroCommand); !ok {
		t.Fatalf("fuel scope should create MacroCommand, got %T", fueled)
	}
	if err := fueled.Execute(); err != nil {
		t.Fatal(err)
	}
	position, _ = ship.Position()
	fuel, _ := ship.Fuel()
	if position != (Vector{X: 3}) || fuel != 4 {
		t.Fatalf("fuel move: position=%+v, fuel=%d", position, fuel)
	}
}

func TestConcurrentScopeSessionsStayIndependent(t *testing.T) {
	const workers = 24
	ioc := NewIoC()
	if err := mustResolveCommand(t, ioc, nil, "IoC.Register", "marker", Factory(func(...any) (any, error) { return -1, nil })).Execute(); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, workers)
	var group sync.WaitGroup
	for index := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			session, err := NewScopeSession(ioc)
			if err != nil {
				results <- err
				return
			}
			name := fmt.Sprintf("game-%d", index)
			newScope, err := ioc.Resolve[Command](session, "Scopes.New", name)
			if err == nil {
				err = newScope.Execute()
			}
			if err == nil {
				current, resolveErr := ioc.Resolve[Command](session, "Scopes.Current", name)
				err = resolveErr
				if err == nil {
					err = current.Execute()
				}
			}
			if err == nil {
				register, resolveErr := ioc.Resolve[Command](session, "IoC.Register", "marker", Factory(func(...any) (any, error) { return index, nil }))
				err = resolveErr
				if err == nil {
					err = register.Execute()
				}
			}
			for attempt := 0; err == nil && attempt < 100; attempt++ {
				var got int
				got, err = ioc.Resolve[int](session, "marker")
				if err == nil && got != index {
					err = fmt.Errorf("scope %s got marker %d", name, got)
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
	root, err := ioc.Resolve[int](nil, "marker")
	if err != nil || root != -1 {
		t.Fatalf("root changed: %d, %v", root, err)
	}
}

func TestConcurrentRegistrationHasOneWinner(t *testing.T) {
	ioc := NewIoC()
	const contenders = 20
	results := make(chan error, contenders)
	var group sync.WaitGroup
	for index := range contenders {
		group.Add(1)
		go func() {
			defer group.Done()
			command, err := ioc.Resolve[Command](nil, "IoC.Register", "shared", Factory(func(...any) (any, error) { return index, nil }))
			if err == nil {
				err = command.Execute()
			}
			results <- err
		}()
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrDuplicateDependency):
		default:
			t.Fatalf("unexpected concurrent registration error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations: %d", successes)
	}
}
