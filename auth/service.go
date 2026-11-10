package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

const maxRequestBytes = 64 << 10

// Registry owns participant lists. This educational implementation is in memory.
type Registry struct {
	mu    sync.RWMutex
	games map[string]map[string]struct{}
}

func NewRegistry() *Registry {
	return &Registry{games: make(map[string]map[string]struct{})}
}

func (registry *Registry) CreateGame(participants []string) (string, error) {
	if registry == nil || len(participants) == 0 {
		return "", errors.New("at least one participant is required")
	}
	users := make(map[string]struct{}, len(participants))
	for _, id := range participants {
		if !validUserID(id) {
			return "", errors.New("invalid participant ID")
		}
		if _, duplicate := users[id]; duplicate {
			return "", errors.New("duplicate participant ID")
		}
		users[id] = struct{}{}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for {
		var randomID [16]byte
		if _, err := rand.Read(randomID[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(randomID[:])
		if _, exists := registry.games[id]; !exists {
			registry.games[id] = users
			return id, nil
		}
	}
}

func (registry *Registry) IsParticipant(gameID, userID string) bool {
	if registry == nil {
		return false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	_, ok := registry.games[gameID][userID]
	return ok
}

// Service creates games and issues access tokens to registered participants.
type Service struct {
	users    *PasswordStore
	registry *Registry
	issuer   *TokenIssuer
}

func NewService(users *PasswordStore, registry *Registry, issuer *TokenIssuer) (*Service, error) {
	if users == nil || registry == nil || issuer == nil {
		return nil, errors.New("auth service dependencies are required")
	}
	return &Service{users: users, registry: registry, issuer: issuer}, nil
}

func (service *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if service == nil {
		http.Error(w, "auth service is not configured", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := service.users.Authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="spacebattle-auth"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/games" {
		service.createGame(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/games/") && strings.HasSuffix(r.URL.Path, "/tokens") {
		gameID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/games/"), "/tokens")
		if len(gameID) != 32 || strings.Contains(gameID, "/") {
			http.NotFound(w, r)
			return
		}
		service.issueToken(w, gameID, userID)
		return
	}
	http.NotFound(w, r)
}

func (service *Service) createGame(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Participants []string `json:"participants"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeJSONError(w, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSONError(w, err)
		return
	}
	if len(input.Participants) == 0 {
		http.Error(w, "participants are required", http.StatusBadRequest)
		return
	}
	for _, participant := range input.Participants {
		if !service.users.Exists(participant) {
			http.Error(w, "participants must be registered users", http.StatusBadRequest)
			return
		}
	}
	id, err := service.registry.CreateGame(input.Participants)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		GameID string `json:"game_id"`
	}{GameID: id})
}

func (service *Service) issueToken(w http.ResponseWriter, gameID, userID string) {
	if !service.registry.IsParticipant(gameID, userID) {
		http.Error(w, "user is not a participant of this game", http.StatusForbidden)
		return
	}
	token, err := service.issuer.Issue(userID, gameID)
	if err != nil {
		http.Error(w, "could not issue token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(service.issuer.TTL().Seconds())})
}

func writeJSONError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "request exceeds 64 KiB", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "invalid JSON request", http.StatusBadRequest)
}
