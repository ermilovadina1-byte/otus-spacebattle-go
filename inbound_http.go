package spacebattle

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const maxAgentMessageBytes = 64 << 10

// GameEndpoint translates HTTP requests into InterpretCommand instances.
// Its game map is copied at construction and never modified afterward.
type GameEndpoint struct {
	games map[string]*Game
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
