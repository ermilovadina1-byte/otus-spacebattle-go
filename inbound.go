package spacebattle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrInvalidAgentMessage = errors.New("invalid agent message")
	ErrInvalidGameConfig   = errors.New("invalid game configuration")
	ErrUnknownObject       = errors.New("game object is not registered")
	ErrUnknownOperation    = errors.New("agent operation is not allowed")
)

// AgentMessage has a stable envelope. Each operation owns the schema of Args.
type AgentMessage struct {
	GameID      string          `json:"game_id"`
	ObjectID    string          `json:"object_id"`
	OperationID string          `json:"operation_id"`
	Args        json.RawMessage `json:"args"`
}

func (message AgentMessage) validate() error {
	if message.GameID == "" || message.ObjectID == "" || message.OperationID == "" {
		return fmt.Errorf("%w: game_id, object_id and operation_id are required", ErrInvalidAgentMessage)
	}
	args := bytes.TrimSpace(message.Args)
	if len(args) < 2 || args[0] != '{' || args[len(args)-1] != '}' || !json.Valid(args) {
		return fmt.Errorf("%w: args must be a JSON object", ErrInvalidAgentMessage)
	}
	return nil
}

// GameConfig is assembled before a game's endpoint starts accepting messages.
// Objects and Operations are copied, so their lookup requires no global lock.
type GameConfig struct {
	Worker     *CommandWorker
	IoC        *IoC
	Session    *ScopeSession
	Objects    map[string]any
	Operations map[string]string // public operation ID -> trusted IoC key
}

type Game struct {
	worker     *CommandWorker
	ioc        *IoC
	session    *ScopeSession
	objects    map[string]any
	operations map[string]string
}

func NewGame(config GameConfig) (*Game, error) {
	if config.Worker == nil || config.IoC == nil || config.IoC.root == nil ||
		config.Session != nil && config.Session.owner != config.IoC ||
		len(config.Objects) == 0 || len(config.Operations) == 0 {
		return nil, ErrInvalidGameConfig
	}
	game := &Game{
		worker:     config.Worker,
		ioc:        config.IoC,
		session:    config.Session,
		objects:    make(map[string]any, len(config.Objects)),
		operations: make(map[string]string, len(config.Operations)),
	}
	for id, object := range config.Objects {
		if id == "" || object == nil {
			return nil, ErrInvalidGameConfig
		}
		game.objects[id] = object
	}
	for id, key := range config.Operations {
		if id == "" || key == "" || reservedIoCKey(key) {
			return nil, ErrInvalidGameConfig
		}
		game.operations[id] = key
	}
	return game, nil
}

// InterpretCommand runs on the addressed game's worker. It translates the
// public operation ID through a trusted allowlist before invoking IoC.
type InterpretCommand struct {
	game    *Game
	message AgentMessage
}

func (command InterpretCommand) Execute() error {
	if command.game == nil {
		return ErrInvalidGameConfig
	}
	if err := command.message.validate(); err != nil {
		return err
	}
	object, ok := command.game.objects[command.message.ObjectID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownObject, command.message.ObjectID)
	}
	key, ok := command.game.operations[command.message.OperationID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownOperation, command.message.OperationID)
	}
	gameCommand, err := command.game.ioc.Resolve[Command](command.game.session, key, object, json.RawMessage(bytes.Clone(command.message.Args)))
	if err != nil {
		return fmt.Errorf("interpret %q: %w", command.message.OperationID, err)
	}
	return command.game.worker.enqueueFollowUp(gameCommand)
}

var _ Command = InterpretCommand{}
