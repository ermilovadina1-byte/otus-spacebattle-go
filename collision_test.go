package spacebattle

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

type collisionTestObject struct {
	position Vector
	readErr  error
}

func (object *collisionTestObject) Position() (Vector, error) {
	return object.position, object.readErr
}

func newCollisionTestIndex(t *testing.T, width float64, offsets []Vector) (*CollisionIndex, *[]CollisionPair, *int) {
	t.Helper()
	var reported []CollisionPair
	checks := 0
	index, err := NewCollisionIndex(width, offsets,
		func(CollisionObject, CollisionObject) (bool, error) {
			checks++
			return true, nil
		},
		func(pair CollisionPair) error {
			reported = append(reported, pair)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	return index, &reported, &checks
}

func TestCollisionNeighborhoodsReplaceOldPairMacrosAfterMove(t *testing.T) {
	index, reported, checks := newCollisionTestIndex(t, 10, []Vector{{}})
	a := &collisionTestObject{position: Vector{X: 1}}
	b := &collisionTestObject{position: Vector{X: 2}}
	c := &collisionTestObject{position: Vector{X: 16}}
	for id, object := range map[string]*collisionTestObject{"a": a, "b": b, "c": c} {
		if err := index.Track(id, object); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if want := []CollisionPair{{FirstID: "a", SecondID: "b"}}; !reflect.DeepEqual(*reported, want) || *checks != 1 {
		t.Fatalf("initial pairs=%v, checks=%d", *reported, *checks)
	}
	*reported = nil
	*checks = 0
	b.position = Vector{X: 15}
	if err := (UpdateNeighborhoods{Index: index, ID: "b"}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if want := []CollisionPair{{FirstID: "b", SecondID: "c"}}; !reflect.DeepEqual(*reported, want) || *checks != 1 {
		t.Fatalf("after move pairs=%v, checks=%d", *reported, *checks)
	}
	*reported = nil
	*checks = 0
	if err := index.Untrack("c"); err != nil {
		t.Fatal(err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 0 || *checks != 0 {
		t.Fatalf("removed object remained in checks: pairs=%v, checks=%d", *reported, *checks)
	}
}

func TestTwoShiftedNeighborhoodsCatchOneBoundaryWithoutDuplicatePair(t *testing.T) {
	index, reported, checks := newCollisionTestIndex(t, 10, []Vector{{}, {X: 5, Y: 5}})
	if err := index.Track("a", &collisionTestObject{position: Vector{X: 9.8, Y: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := index.Track("b", &collisionTestObject{position: Vector{X: 10.2, Y: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := (CheckCollisions{Index: index}).Execute(); err != nil {
		t.Fatal(err)
	}
	if want := []CollisionPair{{FirstID: "a", SecondID: "b"}}; !reflect.DeepEqual(*reported, want) || *checks != 1 {
		t.Fatalf("pairs=%v, checks=%d", *reported, *checks)
	}
}

func TestFourShiftedNeighborhoodsCoverCornerMissedByTwo(t *testing.T) {
	a := &collisionTestObject{position: Vector{X: -0.1, Y: 4.9}}
	b := &collisionTestObject{position: Vector{X: 0.1, Y: 5.1}}
	for _, test := range []struct {
		name    string
		offsets []Vector
		want    int
	}{
		{"two diagonal grids", []Vector{{}, {X: 5, Y: 5}}, 0},
		{"four independent shifts", []Vector{{}, {X: 5}, {Y: 5}, {X: 5, Y: 5}}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, reported, checks := newCollisionTestIndex(t, 10, test.offsets)
			if err := index.Track("a", a); err != nil {
				t.Fatal(err)
			}
			if err := index.Track("b", b); err != nil {
				t.Fatal(err)
			}
			if err := index.RunChecks(); err != nil {
				t.Fatal(err)
			}
			if len(*reported) != test.want || *checks != test.want {
				t.Fatalf("pairs=%v, checks=%d, want %d", *reported, *checks, test.want)
			}
		})
	}
}

func TestShiftedNeighborhoodMacroChangesWhenPeerMovesAcrossBoundary(t *testing.T) {
	index, reported, checks := newCollisionTestIndex(t, 10, []Vector{{}, {X: 5, Y: 5}})
	a := &collisionTestObject{position: Vector{X: 9.8, Y: 1}}
	b := &collisionTestObject{position: Vector{X: 20.2, Y: 1}}
	if err := index.Track("a", a); err != nil {
		t.Fatal(err)
	}
	if err := index.Track("b", b); err != nil {
		t.Fatal(err)
	}
	b.position.X = 10.2
	if err := index.Update("b"); err != nil {
		t.Fatal(err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 1 || *checks != 1 {
		t.Fatalf("new shifted neighborhood: pairs=%v, checks=%d", *reported, *checks)
	}
	*reported = nil
	*checks = 0
	b.position.X = 20.2
	if err := index.Update("b"); err != nil {
		t.Fatal(err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 0 || *checks != 0 {
		t.Fatalf("old shifted macro was not replaced: pairs=%v, checks=%d", *reported, *checks)
	}
}

func TestCollisionUpdateReadFailureLeavesMembershipUnchanged(t *testing.T) {
	index, reported, _ := newCollisionTestIndex(t, 10, []Vector{{}})
	a := &collisionTestObject{position: Vector{X: 1}}
	b := &collisionTestObject{position: Vector{X: 2}}
	if err := index.Track("a", a); err != nil {
		t.Fatal(err)
	}
	if err := index.Track("b", b); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("position unavailable")
	b.position = Vector{X: 21}
	b.readErr = cause
	if err := index.Update("b"); !errors.Is(err, cause) {
		t.Fatalf("read failure: %v", err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 1 {
		t.Fatalf("membership changed after read failure: %v", *reported)
	}
	b.readErr = nil
	*reported = nil
	if err := index.Update("b"); err != nil {
		t.Fatal(err)
	}
	if err := index.RunChecks(); err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 0 {
		t.Fatalf("old neighborhood still checks pair: %v", *reported)
	}
}

func TestMoveUpdateAndCollisionCheckComposeWithExistingMacro(t *testing.T) {
	index, reported, _ := newCollisionTestIndex(t, 10, []Vector{{}})
	stationary, err := NewTorpedo(Vector{X: 1}, Vector{})
	if err != nil {
		t.Fatal(err)
	}
	moving, err := NewTorpedo(Vector{X: 15}, Vector{X: -10})
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Track("stationary", stationary); err != nil {
		t.Fatal(err)
	}
	if err := index.Track("moving", moving); err != nil {
		t.Fatal(err)
	}
	macro, err := NewMacroCommand(Move{Object: moving}, UpdateNeighborhoods{Index: index, ID: "moving"}, CheckCollisions{Index: index})
	if err != nil {
		t.Fatal(err)
	}
	if err := macro.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := []CollisionPair{{FirstID: "moving", SecondID: "stationary"}}; !reflect.DeepEqual(*reported, want) {
		t.Fatalf("collisions after move = %v", *reported)
	}
}

func TestCollisionCheckAndReportErrorsRemainVisible(t *testing.T) {
	cause := errors.New("collision service unavailable")
	for _, test := range []struct {
		name   string
		check  CollisionPredicate
		report CollisionReport
	}{
		{"check", func(CollisionObject, CollisionObject) (bool, error) { return false, cause }, func(CollisionPair) error { return errors.New("report after failed check") }},
		{"report", func(CollisionObject, CollisionObject) (bool, error) { return true, nil }, func(CollisionPair) error { return cause }},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, err := NewCollisionIndex(10, []Vector{{}}, test.check, test.report)
			if err != nil {
				t.Fatal(err)
			}
			_ = index.Track("a", &collisionTestObject{position: Vector{}})
			_ = index.Track("b", &collisionTestObject{position: Vector{}})
			if err := index.RunChecks(); !errors.Is(err, cause) {
				t.Fatalf("cause lost: %v", err)
			}
		})
	}
}

func TestCollisionGridRejectsInvalidInputsAndOverflow(t *testing.T) {
	check := func(CollisionObject, CollisionObject) (bool, error) { return false, nil }
	report := func(CollisionPair) error { return nil }
	for _, test := range []struct {
		width   float64
		offsets []Vector
		check   CollisionPredicate
		report  CollisionReport
	}{
		{0, []Vector{{}}, check, report},
		{math.NaN(), []Vector{{}}, check, report},
		{10, nil, check, report},
		{10, []Vector{{X: math.Inf(1)}}, check, report},
		{10, []Vector{{}}, nil, report},
		{10, []Vector{{}}, check, nil},
	} {
		if _, err := NewCollisionIndex(test.width, test.offsets, test.check, test.report); !errors.Is(err, ErrInvalidCollisionGrid) {
			t.Fatalf("invalid grid accepted: %+v, err=%v", test, err)
		}
	}
	index, _, _ := newCollisionTestIndex(t, 10, []Vector{{}})
	if err := index.Track("", &collisionTestObject{}); !errors.Is(err, ErrInvalidCollisionObject) {
		t.Fatalf("empty ID: %v", err)
	}
	if err := index.Track("a", nil); !errors.Is(err, ErrInvalidCollisionObject) {
		t.Fatalf("nil object: %v", err)
	}
	var typedNil *Torpedo
	if err := index.Track("a", typedNil); !errors.Is(err, ErrInvalidCollisionObject) {
		t.Fatalf("typed nil object: %v", err)
	}
	if err := index.Track("a", &collisionTestObject{position: Vector{X: math.MaxFloat64}}); !errors.Is(err, ErrCollisionCellOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	if err := index.Track("a", &collisionTestObject{}); err != nil {
		t.Fatal(err)
	}
	if err := index.Track("a", &collisionTestObject{}); !errors.Is(err, ErrCollisionObjectExists) {
		t.Fatalf("duplicate ID: %v", err)
	}
	if err := index.Update("missing"); !errors.Is(err, ErrCollisionObjectNotFound) {
		t.Fatalf("missing update: %v", err)
	}
	if err := index.Untrack("missing"); !errors.Is(err, ErrCollisionObjectNotFound) {
		t.Fatalf("missing untrack: %v", err)
	}
}
