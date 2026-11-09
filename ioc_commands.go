package spacebattle

type registerDependency struct {
	container *IoC
	session   *ScopeSession
	key       string
	factory   dependencyFactory
}

func (command *registerDependency) Execute() error {
	scope := currentScope(command.container, command.session)
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if _, exists := scope.factories[command.key]; exists {
		return ErrDuplicateDependency
	}
	scope.factories[command.key] = command.factory
	return nil
}

type createScope struct {
	container *IoC
	session   *ScopeSession
	name      string
}

func (command *createScope) Execute() error {
	parent := currentScope(command.container, command.session)
	command.container.mu.Lock()
	defer command.container.mu.Unlock()
	if _, exists := command.container.scopes[command.name]; exists {
		return ErrDuplicateScope
	}
	command.container.scopes[command.name] = &dependencyScope{
		parent:    parent,
		factories: make(map[string]dependencyFactory),
	}
	return nil
}

type setCurrentScope struct {
	container *IoC
	session   *ScopeSession
	name      string
}

func (command *setCurrentScope) Execute() error {
	command.container.mu.RLock()
	scope, exists := command.container.scopes[command.name]
	command.container.mu.RUnlock()
	if !exists {
		return ErrUnknownScope
	}
	command.session.mu.Lock()
	command.session.current = scope
	command.session.mu.Unlock()
	return nil
}

var _ Command = (*registerDependency)(nil)
var _ Command = (*createScope)(nil)
var _ Command = (*setCurrentScope)(nil)
