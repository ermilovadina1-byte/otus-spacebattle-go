package spacebattle

import (
	"errors"
	"testing"
)

func TestMoveChangesPosition(t *testing.T) {
	object, err := NewTorpedo(Vector{X: 12, Y: 5}, Vector{X: -7, Y: 3})
	if err != nil {
		t.Fatal(err)
	}

	if err := (Move{Object: object}).Execute(); err != nil {
		t.Fatal(err)
	}
	got, err := object.Position()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Vector{X: 5, Y: 8}); got != want {
		t.Fatalf("position = %+v, want %+v", got, want)
	}
}

func TestMoveReportsEachAccessFailure(t *testing.T) {
	readPosition := errors.New("position unavailable")
	readVelocity := errors.New("velocity unavailable")
	writePosition := errors.New("position blocked")
	tests := []struct {
		name      string
		object    *stubMovingObject
		wantError error
		wantSet   int
	}{
		{
			name:      "read position",
			object:    &stubMovingObject{positionError: readPosition},
			wantError: readPosition,
		},
		{
			name:      "read velocity",
			object:    &stubMovingObject{velocityError: readVelocity},
			wantError: readVelocity,
		},
		{
			name:      "write position",
			object:    &stubMovingObject{setError: writePosition},
			wantError: writePosition,
			wantSet:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (Move{Object: tt.object}).Execute()
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.wantError)
			}
			if tt.object.setCalls != tt.wantSet {
				t.Fatalf("SetPosition calls = %d, want %d", tt.object.setCalls, tt.wantSet)
			}
		})
	}
}

func TestMoveRejectsNonFiniteResultBeforeWriting(t *testing.T) {
	object := &stubMovingObject{
		position: Vector{X: 1e308},
		velocity: Vector{X: 1e308},
	}
	if err := (Move{Object: object}).Execute(); !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("error = %v, want ErrInvalidVector", err)
	}
	if object.setCalls != 0 {
		t.Fatalf("SetPosition called %d times", object.setCalls)
	}
}

func TestMoveRejectsMissingObject(t *testing.T) {
	if err := (Move{}).Execute(); !errors.Is(err, ErrNilObject) {
		t.Fatalf("error = %v, want ErrNilObject", err)
	}
}

type stubMovingObject struct {
	position      Vector
	velocity      Vector
	positionError error
	velocityError error
	setError      error
	setCalls      int
}

func (s *stubMovingObject) Position() (Vector, error) {
	return s.position, s.positionError
}

func (s *stubMovingObject) Velocity() (Vector, error) {
	return s.velocity, s.velocityError
}

func (s *stubMovingObject) SetPosition(position Vector) error {
	s.setCalls++
	if s.setError != nil {
		return s.setError
	}
	s.position = position
	return nil
}
