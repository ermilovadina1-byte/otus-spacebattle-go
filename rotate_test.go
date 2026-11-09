package spacebattle

import (
	"errors"
	"math"
	"testing"
)

func TestDirectionWrapsBothWays(t *testing.T) {
	direction, err := NewDirection(7, 8)
	if err != nil {
		t.Fatal(err)
	}
	forward, err := direction.Next(2)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Index() != 1 {
		t.Fatalf("forward index = %d, want 1", forward.Index())
	}
	backward, err := forward.Next(-3)
	if err != nil {
		t.Fatal(err)
	}
	if backward.Index() != 6 {
		t.Fatalf("backward index = %d, want 6", backward.Index())
	}
}

func TestDirectionRejectsZeroSectors(t *testing.T) {
	if _, err := NewDirection(0, 0); !errors.Is(err, ErrInvalidDirection) {
		t.Fatalf("error = %v, want ErrInvalidDirection", err)
	}
}

func TestDirectionLargeRotationDoesNotOverflow(t *testing.T) {
	sectors := int(^uint(0) >> 1)
	direction, err := NewDirection(sectors-2, sectors)
	if err != nil {
		t.Fatal(err)
	}
	next, err := direction.Next(sectors - 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := sectors - 3; next.Index() != want {
		t.Fatalf("direction = %d, want %d", next.Index(), want)
	}
}

func TestRotateChangesDirection(t *testing.T) {
	direction, _ := NewDirection(7, 8)
	object := &stubRotatingObject{direction: direction, angularVelocity: 2}
	if err := (Rotate{Object: object}).Execute(); err != nil {
		t.Fatal(err)
	}
	if object.direction.Index() != 1 {
		t.Fatalf("direction = %d, want 1", object.direction.Index())
	}
}

func TestRotateReportsEachAccessFailure(t *testing.T) {
	direction, _ := NewDirection(0, 8)
	readDirection := errors.New("direction unavailable")
	readAngularVelocity := errors.New("angular velocity unavailable")
	writeDirection := errors.New("direction blocked")
	tests := []struct {
		name      string
		object    *stubRotatingObject
		wantError error
		wantSet   int
	}{
		{
			name:      "read direction",
			object:    &stubRotatingObject{directionError: readDirection},
			wantError: readDirection,
		},
		{
			name:      "read angular velocity",
			object:    &stubRotatingObject{direction: direction, angularVelocityError: readAngularVelocity},
			wantError: readAngularVelocity,
		},
		{
			name:      "write direction",
			object:    &stubRotatingObject{direction: direction, setError: writeDirection},
			wantError: writeDirection,
			wantSet:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (Rotate{Object: tt.object}).Execute()
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.wantError)
			}
			if tt.object.setCalls != tt.wantSet {
				t.Fatalf("SetDirection calls = %d, want %d", tt.object.setCalls, tt.wantSet)
			}
		})
	}
}

func TestRotateRejectsMissingObject(t *testing.T) {
	if err := (Rotate{}).Execute(); !errors.Is(err, ErrNilObject) {
		t.Fatalf("error = %v, want ErrNilObject", err)
	}
}

func TestRotatingShipChangesItsNextMovement(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	ship, err := NewShip(Vector{}, 2, direction, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Move{Object: ship}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := (Rotate{Object: ship}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := (Move{Object: ship}).Execute(); err != nil {
		t.Fatal(err)
	}
	position, err := ship.Position()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(position.X-2) > 1e-12 || math.Abs(position.Y-2) > 1e-12 {
		t.Fatalf("position = %+v, want approximately (2, 2)", position)
	}
}

type stubRotatingObject struct {
	direction            Direction
	angularVelocity      int
	directionError       error
	angularVelocityError error
	setError             error
	setCalls             int
}

func (s *stubRotatingObject) Direction() (Direction, error) {
	return s.direction, s.directionError
}

func (s *stubRotatingObject) AngularVelocity() (int, error) {
	return s.angularVelocity, s.angularVelocityError
}

func (s *stubRotatingObject) SetDirection(direction Direction) error {
	s.setCalls++
	if s.setError != nil {
		return s.setError
	}
	s.direction = direction
	return nil
}
