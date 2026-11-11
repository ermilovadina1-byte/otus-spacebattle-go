package main

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

	spacebattle "github.com/ermilovadina1-byte/otus-spacebattle-go"
	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

func TestDemoRunsAuthorizedOrdersAndSafeSnapshots(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make(map[string]string)
	for _, userID := range []string{"alice", "bob"} {
		hash, err := auth.HashPassword(userID + "-test-password")
		if err != nil {
			t.Fatal(err)
		}
		hashes[userID] = hash
	}
	users, err := auth.NewPasswordStore(hashes)
	if err != nil {
		t.Fatal(err)
	}
	current, err := newDemo(users, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(current.close)
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		current.handler.ServeHTTP(response, req)
		return response
	}
	token := func(userID string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/games/"+current.gameID+"/tokens", nil)
		req.SetBasicAuth(userID, userID+"-test-password")
		response := httptest.NewRecorder()
		current.handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("token for %s: %d %s", userID, response.Code, response.Body.String())
		}
		var payload struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.AccessToken == "" {
			t.Fatalf("decode token for %s: %v", userID, err)
		}
		return payload.AccessToken
	}
	aliceToken := token("alice")
	bobToken := token("bob")
	statePath := "/state?game_id=" + current.gameID
	if response := request(http.MethodGet, statePath, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected state: %d", response.Code)
	}
	issuer, _ := auth.NewTokenIssuer(key, 5*time.Minute)
	otherGameToken, _ := issuer.Issue("alice", "other-game")
	if response := request(http.MethodGet, statePath, "", otherGameToken); response.Code != http.StatusUnauthorized {
		t.Fatalf("foreign game state exposed: %d", response.Code)
	}
	postOrder := func(objectID, operation, args, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		body := fmt.Sprintf(`{"game_id":%q,"object_id":%q,"operation_id":%q,"args":%s}`,
			current.gameID, objectID, operation, args)
		return request(http.MethodPost, "/commands", body, bearer)
	}
	if response := postOrder("ship-alice", "Advance", `{}`, bobToken); response.Code != http.StatusForbidden {
		t.Fatalf("foreign ship accepted: %d", response.Code)
	}
	for _, order := range []struct {
		operation string
		args      string
	}{
		{"StartMove", `{"initialVelocity":2}`},
		{"Advance", `{}`},
	} {
		if response := postOrder("ship-alice", order.operation, order.args, aliceToken); response.Code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", order.operation, response.Code, response.Body.String())
		}
	}
	readState := func() gameState {
		t.Helper()
		response := request(http.MethodGet, statePath, "", aliceToken)
		if response.Code != http.StatusOK {
			t.Fatalf("state: %d %s", response.Code, response.Body.String())
		}
		var state gameState
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := readState() // The snapshot command runs after both accepted orders.
	if state.GameID != current.gameID || len(state.Ships) != 2 ||
		state.Ships[0].Position.X != 2 || state.Ships[0].Velocity.X != 2 ||
		state.Ships[1].Position.X != 10 {
		t.Fatalf("unexpected state after movement: %+v", state)
	}
	if response := postOrder("ship-alice", "StopMove", `{}`, aliceToken); response.Code != http.StatusAccepted {
		t.Fatalf("stop: %d", response.Code)
	}
	if response := postOrder("ship-alice", "Advance", `{}`, aliceToken); response.Code != http.StatusAccepted {
		t.Fatalf("advance after stop: %d", response.Code)
	}
	state = readState()
	if state.Ships[0].Position.X != 2 || state.Ships[0].Velocity.X != 0 {
		t.Fatalf("stop did not hold position: %+v", state.Ships[0])
	}
	if response := postOrder("ship-alice", "Advance", `{"unexpected":1}`, aliceToken); response.Code != http.StatusAccepted {
		t.Fatalf("bad arguments should be accepted for later validation: %d", response.Code)
	}
	state = readState()
	if state.Ships[0].Position.X != 2 {
		t.Fatalf("bad arguments changed state: %+v", state.Ships[0])
	}
	if failures := current.worker.Failures(); len(failures) != 1 || !errors.Is(failures[0].Err, spacebattle.ErrInvalidArguments) {
		t.Fatalf("late validation failure was not recorded: %+v", failures)
	}
}

func TestDemoRejectsNonLoopbackListener(t *testing.T) {
	if err := run([]string{"-listen", "0.0.0.0:8096"}); err == nil {
		t.Fatal("non-loopback listener was accepted")
	}
}
