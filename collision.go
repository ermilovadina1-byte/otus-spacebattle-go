package spacebattle

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
)

var (
	ErrNilCollisionIndex       = errors.New("collision index is nil")
	ErrInvalidCollisionGrid    = errors.New("collision grid needs a positive finite cell width, at least one finite offset, and collision callbacks")
	ErrInvalidCollisionObject  = errors.New("collision object needs a non-empty ID and a position")
	ErrCollisionObjectExists   = errors.New("collision object ID already exists")
	ErrCollisionObjectNotFound = errors.New("collision object ID is not tracked")
	ErrCollisionCellOverflow   = errors.New("collision cell coordinate is out of range")
)

// CollisionObject supplies the center used by the neighborhood index.
// The caller's collision test may inspect a concrete object's other properties.
type CollisionObject interface {
	Position() (Vector, error)
}

// CollisionPair has stable ID order so a pair is tested only once per scan.
type CollisionPair struct {
	FirstID  string
	SecondID string
}

type CollisionPredicate func(first, second CollisionObject) (bool, error)
type CollisionReport func(pair CollisionPair) error

type collisionCell struct{ x, y int64 }

type collisionLayer struct {
	offset     Vector
	cells      map[collisionCell]map[string]struct{}
	membership map[string]collisionCell
}

// Every handler contributes candidates from one grid, then passes the request
// to the next grid. A set removes repeats across overlapping neighborhoods.
type collisionCandidateHandler interface {
	collect(string, map[string]struct{})
}

type gridCandidateHandler struct {
	layer *collisionLayer
	next  collisionCandidateHandler
}

func (handler *gridCandidateHandler) collect(id string, peers map[string]struct{}) {
	cell := handler.layer.membership[id]
	for peer := range handler.layer.cells[cell] {
		if id < peer {
			peers[peer] = struct{}{}
		}
	}
	if handler.next != nil {
		handler.next.collect(id, peers)
	}
}

// CollisionIndex owns several shifted square grids and one collision macro per
// object. Use it from one game worker; neither the index nor game objects are
// synchronized for concurrent mutation.
type CollisionIndex struct {
	width   float64
	layers  []collisionLayer
	objects map[string]CollisionObject
	macros  map[string]*MacroCommand
	chain   collisionCandidateHandler
	check   CollisionPredicate
	report  CollisionReport
}

// NewCollisionIndex accepts any positive number of grid offsets. Four offsets
// (0,0), (w/2,0), (0,w/2), (w/2,w/2) cover pairs whose center separation is
// strictly less than w/2 on both axes. The exact collision rule is injected.
func NewCollisionIndex(width float64, offsets []Vector, check CollisionPredicate, report CollisionReport) (*CollisionIndex, error) {
	if !finite(width) || width <= 0 || len(offsets) == 0 || check == nil || report == nil {
		return nil, ErrInvalidCollisionGrid
	}
	index := &CollisionIndex{
		width:   width,
		layers:  make([]collisionLayer, len(offsets)),
		objects: make(map[string]CollisionObject),
		macros:  make(map[string]*MacroCommand),
		check:   check,
		report:  report,
	}
	for i, offset := range offsets {
		if !offset.valid() {
			return nil, ErrInvalidCollisionGrid
		}
		index.layers[i] = collisionLayer{
			offset:     offset,
			cells:      make(map[collisionCell]map[string]struct{}),
			membership: make(map[string]collisionCell),
		}
	}
	for i := len(index.layers) - 1; i >= 0; i-- {
		index.chain = &gridCandidateHandler{layer: &index.layers[i], next: index.chain}
	}
	return index, nil
}

// Track registers an object at its current position and refreshes the macros
// of objects sharing a cell with it.
func (index *CollisionIndex) Track(id string, object CollisionObject) error {
	if index == nil {
		return ErrNilCollisionIndex
	}
	if id == "" || nilCollisionObject(object) {
		return ErrInvalidCollisionObject
	}
	if _, exists := index.objects[id]; exists {
		return ErrCollisionObjectExists
	}
	position, err := object.Position()
	if err != nil {
		return fmt.Errorf("read collision position for %q: %w", id, err)
	}
	cells, err := index.cellsFor(position)
	if err != nil {
		return err
	}
	index.objects[id] = object
	affected := map[string]struct{}{id: {}}
	for i, cell := range cells {
		layer := &index.layers[i]
		members := layer.cells[cell]
		if members == nil {
			members = make(map[string]struct{})
			layer.cells[cell] = members
		}
		addMembers(affected, members)
		members[id] = struct{}{}
		layer.membership[id] = cell
	}
	index.rebuildMacros(affected)
	return nil
}

// Update refreshes the object's membership after its position changes. All
// cell coordinates are validated before changing any layer.
func (index *CollisionIndex) Update(id string) error {
	if index == nil {
		return ErrNilCollisionIndex
	}
	object, exists := index.objects[id]
	if !exists {
		return ErrCollisionObjectNotFound
	}
	position, err := object.Position()
	if err != nil {
		return fmt.Errorf("read collision position for %q: %w", id, err)
	}
	cells, err := index.cellsFor(position)
	if err != nil {
		return err
	}
	affected := map[string]struct{}{id: {}}
	changed := false
	for i, next := range cells {
		layer := &index.layers[i]
		previous := layer.membership[id]
		if previous == next {
			continue
		}
		changed = true
		addMembers(affected, layer.cells[previous])
		addMembers(affected, layer.cells[next])
		delete(layer.cells[previous], id)
		if len(layer.cells[previous]) == 0 {
			delete(layer.cells, previous)
		}
		if layer.cells[next] == nil {
			layer.cells[next] = make(map[string]struct{})
		}
		layer.cells[next][id] = struct{}{}
		layer.membership[id] = next
	}
	if changed {
		index.rebuildMacros(affected)
	}
	return nil
}

// Untrack removes an object and updates the remaining collision macros.
func (index *CollisionIndex) Untrack(id string) error {
	if index == nil {
		return ErrNilCollisionIndex
	}
	if _, exists := index.objects[id]; !exists {
		return ErrCollisionObjectNotFound
	}
	affected := make(map[string]struct{})
	for i := range index.layers {
		layer := &index.layers[i]
		cell := layer.membership[id]
		addMembers(affected, layer.cells[cell])
		delete(layer.cells[cell], id)
		if len(layer.cells[cell]) == 0 {
			delete(layer.cells, cell)
		}
		delete(layer.membership, id)
	}
	delete(affected, id)
	delete(index.objects, id)
	delete(index.macros, id)
	index.rebuildMacros(affected)
	return nil
}

// RunChecks executes each current pair once, in stable ID order. On a check
// or report error it stops, like the existing MacroCommand.
func (index *CollisionIndex) RunChecks() error {
	if index == nil {
		return ErrNilCollisionIndex
	}
	ids := make([]string, 0, len(index.objects))
	for id := range index.objects {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := index.macros[id].Execute(); err != nil {
			return fmt.Errorf("collision checks for %q: %w", id, err)
		}
	}
	return nil
}

// UpdateNeighborhoods and CheckCollisions fit the existing Command API.
type UpdateNeighborhoods struct {
	Index *CollisionIndex
	ID    string
}

func (command UpdateNeighborhoods) Execute() error { return command.Index.Update(command.ID) }

type CheckCollisions struct{ Index *CollisionIndex }

func (command CheckCollisions) Execute() error { return command.Index.RunChecks() }

func (index *CollisionIndex) cellsFor(position Vector) ([]collisionCell, error) {
	if !position.valid() {
		return nil, ErrInvalidVector
	}
	cells := make([]collisionCell, len(index.layers))
	for i, layer := range index.layers {
		x, err := cellCoordinate(position.X, layer.offset.X, index.width)
		if err != nil {
			return nil, err
		}
		y, err := cellCoordinate(position.Y, layer.offset.Y, index.width)
		if err != nil {
			return nil, err
		}
		cells[i] = collisionCell{x: x, y: y}
	}
	return cells, nil
}

func cellCoordinate(value, offset, width float64) (int64, error) {
	const limit = float64(uint64(1) << 63)
	coordinate := math.Floor((value - offset) / width)
	if !finite(coordinate) || coordinate < -limit || coordinate >= limit {
		return 0, ErrCollisionCellOverflow
	}
	return int64(coordinate), nil
}

func addMembers(destination map[string]struct{}, members map[string]struct{}) {
	for id := range members {
		destination[id] = struct{}{}
	}
}

func nilCollisionObject(object CollisionObject) bool {
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

func (index *CollisionIndex) rebuildMacros(affected map[string]struct{}) {
	for id := range affected {
		if _, tracked := index.objects[id]; !tracked {
			continue
		}
		peers := make(map[string]struct{})
		index.chain.collect(id, peers)
		peerIDs := make([]string, 0, len(peers))
		for peer := range peers {
			peerIDs = append(peerIDs, peer)
		}
		slices.Sort(peerIDs)
		commands := make([]Command, 0, len(peerIDs))
		for _, peer := range peerIDs {
			commands = append(commands, collisionPairCommand{
				pair:   CollisionPair{FirstID: id, SecondID: peer},
				first:  index.objects[id],
				second: index.objects[peer],
				check:  index.check,
				report: index.report,
			})
		}
		index.macros[id] = &MacroCommand{commands: commands}
	}
}

type collisionPairCommand struct {
	pair   CollisionPair
	first  CollisionObject
	second CollisionObject
	check  CollisionPredicate
	report CollisionReport
}

func (command collisionPairCommand) Execute() error {
	collided, err := command.check(command.first, command.second)
	if err != nil {
		return fmt.Errorf("check pair %q and %q: %w", command.pair.FirstID, command.pair.SecondID, err)
	}
	if collided {
		if err := command.report(command.pair); err != nil {
			return fmt.Errorf("report pair %q and %q: %w", command.pair.FirstID, command.pair.SecondID, err)
		}
	}
	return nil
}

var _ Command = UpdateNeighborhoods{}
var _ Command = CheckCollisions{}
var _ Command = collisionPairCommand{}
