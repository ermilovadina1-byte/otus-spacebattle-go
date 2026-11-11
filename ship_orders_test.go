package spacebattle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

func TestShipOrdersRunThroughInterpreterAndRejectOtherOwner(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	aliceBase, _ := NewShip(Vector{}, 0, direction, 0)
	bobBase, _ := NewShip(Vector{X: 10}, 0, direction, 0)
	aliceShots := 0
	bobShots := 0
	alice, _ := NewArmedShip(aliceBase, func() error { aliceShots++; return nil })
	bob, _ := NewArmedShip(bobBase, func() error { bobShots++; return nil })

	ioc := NewIoC()
	registerMoveFactory(t, ioc)
	operations, err := RegisterShipOrderOperations(ioc, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations["move"] = "game.move"
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{
		Worker: worker, IoC: ioc,
		Objects:    map[string]any{"alice-ship": alice, "bob-ship": bob},
		Operations: operations,
	})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := auth.NewTokenIssuer(privateKey, 5*time.Minute)
	verifier, _ := auth.NewTokenVerifier(publicKey)
	endpoint, err := NewAuthorizedGameEndpoint(
		map[string]*Game{"game": game}, verifier,
		map[string]map[string]string{"game": {"alice-ship": "alice", "bob-ship": "bob"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	aliceToken, err := issuer.Issue("alice", "game")
	if err != nil {
		t.Fatal(err)
	}
	post := func(objectID, operation, args string) *httptest.ResponseRecorder {
		t.Helper()
		body := fmt.Appendf(nil, `{"game_id":"game","object_id":%q,"operation_id":%q,"args":%s}`, objectID, operation, args)
		request := httptest.NewRequest(http.MethodPost, "/commands", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+aliceToken)
		response := httptest.NewRecorder()
		endpoint.ServeHTTP(response, request)
		return response
	}
	for _, order := range []struct{ operation, args string }{
		{"StartMove", `{"initialVelocity":2}`},
		{"move", `{}`},
		{"StopMove", `{}`},
		{"move", `{}`},
		{"Shoot", `{}`},
	} {
		if response := post("alice-ship", order.operation, order.args); response.Code != http.StatusAccepted {
			t.Fatalf("%s: status=%d, body=%s", order.operation, response.Code, response.Body.String())
		}
	}
	for _, operation := range []string{"StartMove", "Shoot"} {
		if response := post("bob-ship", operation, `{}`); response.Code != http.StatusForbidden {
			t.Fatalf("foreign %s: status=%d, body=%s", operation, response.Code, response.Body.String())
		}
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	drainWorker(t, worker)
	alicePosition, _ := alice.Position()
	bobPosition, _ := bob.Position()
	aliceVelocity, _ := alice.Velocity()
	if alicePosition != (Vector{X: 2}) || bobPosition != (Vector{X: 10}) ||
		aliceVelocity != (Vector{}) || aliceShots != 1 || bobShots != 0 ||
		len(worker.Failures()) != 0 {
		t.Fatalf("position=%+v, bob=%+v, velocity=%+v, shots=%d/%d, failures=%v",
			alicePosition, bobPosition, aliceVelocity, aliceShots, bobShots, worker.Failures())
	}
}

func TestShipOrderRejectsInvalidPayloadWithoutChangingState(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	ship, _ := NewShip(Vector{}, 1, direction, 0)
	ioc := NewIoC()
	operations, err := RegisterShipOrderOperations(ioc, nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{
		Worker: worker, IoC: ioc,
		Objects: map[string]any{"ship": ship}, Operations: operations,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	for _, payload := range []string{
		`{"initialVelocity":0}`,
		`{"initialVelocity":-1}`,
		`{"initialVelocity":2,"unexpected":true}`,
		`{}`,
	} {
		body := fmt.Appendf(nil, `{"game_id":"game","object_id":"ship","operation_id":"StartMove","args":%s}`, payload)
		if response := postAgentMessage(endpoint, body); response.Code != http.StatusAccepted {
			t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
		}
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	drainWorker(t, worker)
	velocity, _ := ship.Velocity()
	if velocity != (Vector{X: 1}) || len(worker.Failures()) != 4 {
		t.Fatalf("velocity=%+v, failures=%v", velocity, worker.Failures())
	}
	if !errors.Is(worker.Failures()[0].Err, ErrInvalidStartSpeed) {
		t.Fatalf("first failure=%v", worker.Failures()[0].Err)
	}
}

func TestShipOrderPropagatesWeaponFailure(t *testing.T) {
	direction, _ := NewDirection(0, 4)
	base, _ := NewShip(Vector{}, 0, direction, 0)
	if _, err := NewArmedShip(base, nil); !errors.Is(err, ErrNilWeapon) {
		t.Fatalf("missing weapon: %v", err)
	}
	if err := (&ArmedShip{}).Fire(); !errors.Is(err, ErrNilWeapon) {
		t.Fatalf("uninitialized weapon: %v", err)
	}
	failure := errors.New("weapon jammed")
	ship, err := NewArmedShip(base, func() error { return failure })
	if err != nil {
		t.Fatal(err)
	}
	command, err := shootOrderFactory(ship, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := command.(Command).Execute(); !errors.Is(err, failure) {
		t.Fatalf("shoot error=%v", err)
	}
}
