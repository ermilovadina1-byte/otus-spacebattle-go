package spacebattle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

func TestAuthorizedGameEndpointChecksGameAndObjectOwner(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := auth.NewTokenIssuer(privateKey, 5*time.Minute)
	verifier, _ := auth.NewTokenVerifier(publicKey)
	first, _ := NewTorpedo(Vector{}, Vector{X: 1})
	second, _ := NewTorpedo(Vector{}, Vector{X: 1})
	ioc := NewIoC()
	registerMoveFactory(t, ioc)
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{
		Worker: worker, IoC: ioc,
		Objects:    map[string]any{"ship-1": first, "ship-2": second},
		Operations: map[string]string{"move": "game.move"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuthorizedGameEndpoint(map[string]*Game{"game-a": game}, verifier,
		map[string]map[string]string{"game-a": {"ship-1": "alice"}}); err == nil {
		t.Fatal("incomplete object ownership was accepted")
	}
	owners := map[string]map[string]string{"game-a": {"ship-1": "alice", "ship-2": "bob"}}
	endpoint, err := NewAuthorizedGameEndpoint(map[string]*Game{"game-a": game}, verifier, owners)
	if err != nil {
		t.Fatal(err)
	}
	owners["game-a"]["ship-1"] = "bob" // The endpoint must retain its own copy.
	requestBody := []byte(`{"game_id":"game-a","object_id":"ship-1","operation_id":"move","args":{}}`)
	post := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/commands", bytes.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		endpoint.ServeHTTP(response, request)
		return response
	}
	if response := post(""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", response.Code)
	}
	duplicateAuth := httptest.NewRequest(http.MethodPost, "/commands", bytes.NewReader(requestBody))
	duplicateAuth.Header.Set("Content-Type", "application/json")
	duplicateAuth.Header.Add("Authorization", "Bearer first")
	duplicateAuth.Header.Add("Authorization", "Bearer second")
	duplicateResponse := httptest.NewRecorder()
	endpoint.ServeHTTP(duplicateResponse, duplicateAuth)
	if duplicateResponse.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate authorization headers: %d", duplicateResponse.Code)
	}
	otherGameToken, _ := issuer.Issue("alice", "game-b")
	if response := post(otherGameToken); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong game token: %d", response.Code)
	}
	bobToken, _ := issuer.Issue("bob", "game-a")
	if response := post(bobToken); response.Code != http.StatusForbidden {
		t.Fatalf("other user's ship: %d", response.Code)
	}
	aliceToken, _ := issuer.Issue("alice", "game-a")
	if response := post(aliceToken); response.Code != http.StatusAccepted {
		t.Fatalf("owner command: %d %s", response.Code, response.Body.String())
	}
	drainWorker(t, worker)
	position, _ := first.Position()
	otherPosition, _ := second.Position()
	if position != (Vector{X: 1}) || otherPosition != (Vector{}) {
		t.Fatalf("positions after authorized request: first=%+v second=%+v", position, otherPosition)
	}
}
