package spacebattle

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrNilIoC              = errors.New("IoC is nil or uninitialized")
	ErrInvalidScopeSession = errors.New("scope session belongs to another IoC")
	ErrUnknownScope        = errors.New("scope is not registered")
	ErrDuplicateScope      = errors.New("scope already exists")
	ErrUnknownDependency   = errors.New("dependency is not registered")
	ErrDuplicateDependency = errors.New("dependency is already registered in this scope")
	ErrInvalidArguments    = errors.New("invalid Resolve arguments")
	ErrTypeMismatch        = errors.New("resolved value has a different type")
	ErrReservedKey         = errors.New("dependency key is reserved by IoC")
)

// Factory builds a dependency from the arguments supplied to Resolve.
type Factory func(args ...any) (any, error)

type dependencyScope struct {
	parent    *dependencyScope
	mu        sync.RWMutex
	factories map[string]Factory
}

// IoC holds the shared registration graph. Its only public operation is Resolve.
type IoC struct {
	mu     sync.RWMutex
	root   *dependencyScope
	scopes map[string]*dependencyScope
}

func NewIoC() *IoC {
	root := &dependencyScope{factories: make(map[string]Factory)}
	return &IoC{root: root, scopes: map[string]*dependencyScope{"root": root}}
}

// ScopeSession selects a scope for one independent game flow. Pass a separate
// session to each concurrent flow instead of relying on goroutine-local state.
type ScopeSession struct {
	owner   *IoC
	mu      sync.RWMutex
	current *dependencyScope
}

func NewScopeSession(ioc *IoC) (*ScopeSession, error) {
	if ioc == nil || ioc.root == nil {
		return nil, ErrNilIoC
	}
	return &ScopeSession{owner: ioc, current: ioc.root}, nil
}

// Resolve handles dependency lookup and the three configuration operations.
// A nil session selects the root scope. Scope switching requires a session.
func (ioc *IoC) Resolve[T any](session *ScopeSession, key string, args ...any) (T, error) {
	var zero T
	if ioc == nil || ioc.root == nil {
		return zero, ErrNilIoC
	}
	if session != nil && session.owner != ioc {
		return zero, ErrInvalidScopeSession
	}
	if key == "" {
		return zero, ErrInvalidArguments
	}
	value, err := resolveDependency(ioc, session, key, args)
	if err != nil {
		return zero, err
	}
	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("%w: key %q", ErrTypeMismatch, key)
	}
	return result, nil
}

func resolveDependency(ioc *IoC, session *ScopeSession, key string, args []any) (any, error) {
	switch key {
	case "IoC.Register":
		if len(args) != 2 {
			return nil, ErrInvalidArguments
		}
		name, ok := args[0].(string)
		if !ok || name == "" {
			return nil, ErrInvalidArguments
		}
		var factory Factory
		switch f := args[1].(type) {
		case Factory:
			factory = f
		case func(...any) (any, error):
			factory = Factory(f)
		default:
			return nil, ErrInvalidArguments
		}
		if factory == nil {
			return nil, ErrInvalidArguments
		}
		if reservedIoCKey(name) {
			return nil, ErrReservedKey
		}
		return &registerDependency{container: ioc, session: session, key: name, factory: factory}, nil
	case "Scopes.New":
		name, err := scopeName(args)
		if err != nil {
			return nil, err
		}
		if name == "root" {
			return nil, ErrReservedKey
		}
		return &createScope{container: ioc, session: session, name: name}, nil
	case "Scopes.Current":
		name, err := scopeName(args)
		if err != nil {
			return nil, err
		}
		if session == nil {
			return nil, ErrInvalidArguments
		}
		return &setCurrentScope{container: ioc, session: session, name: name}, nil
	}

	scope := currentScope(ioc, session)
	for scope != nil {
		scope.mu.RLock()
		factory, ok := scope.factories[key]
		scope.mu.RUnlock()
		if ok {
			value, err := factory(args...)
			if err != nil {
				return nil, fmt.Errorf("resolve %q: %w", key, err)
			}
			return value, nil
		}
		scope = scope.parent
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownDependency, key)
}

func currentScope(ioc *IoC, session *ScopeSession) *dependencyScope {
	if session == nil {
		return ioc.root
	}
	session.mu.RLock()
	scope := session.current
	session.mu.RUnlock()
	return scope
}

func scopeName(args []any) (string, error) {
	if len(args) != 1 {
		return "", ErrInvalidArguments
	}
	name, ok := args[0].(string)
	if !ok || name == "" {
		return "", ErrInvalidArguments
	}
	return name, nil
}

func reservedIoCKey(key string) bool {
	return strings.HasPrefix(key, "IoC.") || strings.HasPrefix(key, "Scopes.")
}
