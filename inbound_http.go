package spacebattle

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

const maxAgentMessageBytes = 64 << 10

// GameEndpoint translates HTTP requests into InterpretCommand instances.
// Its game map is copied at construction and never modified afterward.
type GameEndpoint struct {
	games    map[string]*Game
	verifier *auth.TokenVerifier
	owners   map[string]map[string]string // game ID -> object ID -> user ID
}

func NewGameEndpoint(games map[string]*Game) (*GameEndpoint, error) {
	if len(games) == 0 {
		return nil, ErrInvalidGameConfig
	}
	endpoint := &GameEndpoint{games: make(map[string]*Game, len(games))}
	for id, game := range games {
		if id == "" || game == nil {
			return nil, ErrInvalidGameConfig
		}
		endpoint.games[id] = game
	}
	return endpoint, nil
}

// NewAuthorizedGameEndpoint requires a signed token for the addressed game and
// checks that its subject owns the addressed object. The old constructor remains
// available for in-process exercises, not for a public server.
func NewAuthorizedGameEndpoint(games map[string]*Game, verifier *auth.TokenVerifier, owners map[string]map[string]string) (*GameEndpoint, error) {
	endpoint, err := NewGameEndpoint(games)
	if err != nil || verifier == nil || len(owners) != len(endpoint.games) {
		return nil, ErrInvalidGameConfig
	}
	endpoint.verifier = verifier
	endpoint.owners = make(map[string]map[string]string, len(owners))
	for gameID, game := range endpoint.games {
		configured, ok := owners[gameID]
		if !ok || len(configured) != len(game.objects) {
			return nil, ErrInvalidGameConfig
		}
		copied := make(map[string]string, len(configured))
		for objectID := range game.objects {
			userID := configured[objectID]
			if userID == "" {
				return nil, ErrInvalidGameConfig
			}
			copied[objectID] = userID
		}
		endpoint.owners[gameID] = copied
	}
	return endpoint, nil
}

func (endpoint *GameEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if endpoint == nil {
		http.Error(w, "game endpoint is not configured", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentMessageBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var message AgentMessage
	if err := decoder.Decode(&message); err != nil {
		writeDecodeError(w, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			http.Error(w, "one JSON object required", http.StatusBadRequest)
			return
		}
		writeDecodeError(w, err)
		return
	}
	if err := message.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if endpoint.verifier != nil {
		if len(r.Header.Values("Authorization")) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "game token required", http.StatusUnauthorized)
			return
		}
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 8192 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "game token required", http.StatusUnauthorized)
			return
		}
		userID, err := endpoint.verifier.Verify(parts[1], message.GameID)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "invalid game token", http.StatusUnauthorized)
			return
		}
		if endpoint.owners[message.GameID][message.ObjectID] != userID {
			http.Error(w, "object is not controlled by this user", http.StatusForbidden)
			return
		}
	}
	game, ok := endpoint.games[message.GameID]
	if !ok {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	if err := game.worker.Enqueue(InterpretCommand{game: game, message: message}); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "message exceeds 64 KiB", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "invalid JSON message", http.StatusBadRequest)
}
