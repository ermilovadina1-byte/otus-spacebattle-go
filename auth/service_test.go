package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func TestAuthServiceCreatesGameAndIssuesParticipantTokens(t *testing.T) {
	publicKey, privateKey := testKeys(t)
	issuer, err := NewTokenIssuer(privateKey, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewTokenVerifier(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make(map[string]string)
	for _, id := range []string{"alice", "bob", "mallory"} {
		hashes[id], err = HashPassword(id + "-password")
		if err != nil {
			t.Fatal(err)
		}
	}
	users, err := NewPasswordStore(hashes)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(users, NewRegistry(), issuer)
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, body, user, password string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if user != "" {
			request.SetBasicAuth(user, password)
		}
		response := httptest.NewRecorder()
		service.ServeHTTP(response, request)
		return response
	}
	gameBody := `{"participants":["alice","bob"]}`
	if response := post("/games", gameBody, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing credentials: %d", response.Code)
	}
	if response := post("/games", gameBody, "alice", "wrong"); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", response.Code)
	}
	duplicateAuth := httptest.NewRequest(http.MethodPost, "/games", strings.NewReader(gameBody))
	duplicateAuth.Header.Set("Content-Type", "application/json")
	duplicateAuth.SetBasicAuth("alice", "alice-password")
	duplicateAuth.Header.Add("Authorization", "Basic duplicate")
	duplicateResponse := httptest.NewRecorder()
	service.ServeHTTP(duplicateResponse, duplicateAuth)
	if duplicateResponse.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate authorization headers: %d", duplicateResponse.Code)
	}
	if response := post("/games", `{"participants":["alice","unknown"]}`, "alice", "alice-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown participant: %d", response.Code)
	}
	response := post("/games", gameBody, "alice", "alice-password")
	if response.Code != http.StatusCreated {
		t.Fatalf("create game: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		GameID string `json:"game_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || len(created.GameID) != 32 {
		t.Fatalf("game response: %+v, %v", created, err)
	}
	if response := post("/games/"+created.GameID+"/tokens", "", "mallory", "mallory-password"); response.Code != http.StatusForbidden {
		t.Fatalf("nonparticipant token: %d", response.Code)
	}
	for _, id := range []string{"alice", "bob"} {
		response := post("/games/"+created.GameID+"/tokens", "", id, id+"-password")
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s token: %d %s", id, response.Code, response.Body.String())
		}
		var issued struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &issued); err != nil || issued.TokenType != "Bearer" || issued.ExpiresIn != 300 {
			t.Fatalf("%s response: %+v, %v", id, issued, err)
		}
		actualID, err := verifier.Verify(issued.AccessToken, created.GameID)
		if err != nil || actualID != id {
			t.Fatalf("%s token verification: %s, %v", id, actualID, err)
		}
	}
}

func TestTokenVerifierRejectsChangedScopeAndSignature(t *testing.T) {
	publicKey, privateKey := testKeys(t)
	issuer, _ := NewTokenIssuer(privateKey, time.Minute)
	verifier, _ := NewTokenVerifier(publicKey)
	valid, err := issuer.Issue("alice", "game-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(valid, "game-b"); err != ErrInvalidToken {
		t.Fatalf("wrong game accepted: %v", err)
	}
	parts := strings.Split(valid, ".")
	changed := []byte(parts[2])
	if changed[0] == 'A' {
		changed[0] = 'B'
	} else {
		changed[0] = 'A'
	}
	parts[2] = string(changed)
	if _, err := verifier.Verify(strings.Join(parts, "."), "game-a"); err != ErrInvalidToken {
		t.Fatalf("changed signature accepted: %v", err)
	}
	claims := GameClaims{GameID: "game-a", RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuerID, Audience: jwt.ClaimStrings{audience}, Subject: "alice",
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}}
	expired := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	expired.Header["typ"] = tokenType
	rawExpired, err := expired.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(rawExpired, "game-a"); err != ErrInvalidToken {
		t.Fatalf("expired token accepted: %v", err)
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	claims.IssuedAt = jwt.NewNumericDate(time.Now())
	wrongType := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	wrongType.Header["typ"] = "JWT"
	rawWrongType, err := wrongType.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(rawWrongType, "game-a"); err != ErrInvalidToken {
		t.Fatalf("wrong type accepted: %v", err)
	}
	claims.Audience = jwt.ClaimStrings{"another-service"}
	wrongAudience := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	wrongAudience.Header["typ"] = tokenType
	rawWrongAudience, err := wrongAudience.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(rawWrongAudience, "game-a"); err != ErrInvalidToken {
		t.Fatalf("wrong audience accepted: %v", err)
	}
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	wrongAlgorithm.Header["typ"] = tokenType
	rawWrongAlgorithm, err := wrongAlgorithm.SignedString([]byte("test-only-hmac-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(rawWrongAlgorithm, "game-a"); err != ErrInvalidToken {
		t.Fatalf("wrong algorithm accepted: %v", err)
	}
}
