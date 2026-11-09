package spacebattle

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var ErrUnknownAdapterType = errors.New("adapter interface is not registered")
var ErrDuplicateAdapterType = errors.New("adapter interface is already registered")

type adapterConstructor func(*IoC, *ScopeSession, any) any

// AdapterRegistry collects generated constructors before installing one IoC strategy.
type AdapterRegistry struct {
	mu           sync.RWMutex
	constructors map[reflect.Type]adapterConstructor
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{constructors: make(map[reflect.Type]adapterConstructor)}
}

func (registry *AdapterRegistry) Register(target reflect.Type, constructor func(*IoC, *ScopeSession, any) any) error {
	if registry == nil || target == nil || target.Kind() != reflect.Interface || constructor == nil {
		return ErrInvalidArguments
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.constructors[target]; exists {
		return ErrDuplicateAdapterType
	}
	registry.constructors[target] = constructor
	return nil
}

// Install registers the "Adapter" dependency in the selected scope.
// The installed strategy captures a snapshot of constructors, while each
// adapter receives the session of its own Resolve call.
func (registry *AdapterRegistry) Install(ioc *IoC, session *ScopeSession) error {
	if registry == nil {
		return ErrInvalidArguments
	}
	registry.mu.RLock()
	constructors := make(map[reflect.Type]adapterConstructor, len(registry.constructors))
	for target, constructor := range registry.constructors {
		constructors[target] = constructor
	}
	registry.mu.RUnlock()

	factory := ContextFactory(func(container *IoC, caller *ScopeSession, args ...any) (any, error) {
		if len(args) != 2 {
			return nil, ErrInvalidArguments
		}
		target, ok := args[0].(reflect.Type)
		if !ok || target == nil || nilAdapterObject(args[1]) {
			return nil, ErrInvalidArguments
		}
		constructor, exists := constructors[target]
		if !exists {
			return nil, fmt.Errorf("%w: %v", ErrUnknownAdapterType, target)
		}
		return constructor(container, caller, args[1]), nil
	})
	command, err := ioc.Resolve[Command](session, "IoC.Register", "Adapter", factory)
	if err != nil {
		return err
	}
	return command.Execute()
}

func nilAdapterObject(object any) bool {
	if object == nil {
		return true
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
