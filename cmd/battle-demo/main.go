// battle-demo runs one ephemeral game on loopback for an end-to-end local demo.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	spacebattle "github.com/ermilovadina1-byte/otus-spacebattle-go"
	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

type point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type shipState struct {
	ObjectID  string `json:"object_id"`
	OwnerID   string `json:"owner_id"`
	Position  point  `json:"position"`
	Velocity  point  `json:"velocity"`
	Direction int    `json:"direction"`
}

type gameState struct {
	GameID string      `json:"game_id"`
	Ships  []shipState `json:"ships"`
}

type snapshotResult struct {
	state gameState
	err   error
}

type demoShip struct {
	id    string
	owner string
	ship  *spacebattle.Ship
}

type snapshotCommand struct {
	gameID string
	ships  []demoShip
	result chan snapshotResult
}

func (command snapshotCommand) Execute() error {
	state := gameState{GameID: command.gameID, Ships: make([]shipState, 0, len(command.ships))}
	for _, item := range command.ships {
		position, err := item.ship.Position()
		if err != nil {
			command.result <- snapshotResult{err: err}
			return nil
		}
		velocity, err := item.ship.Velocity()
		if err != nil {
			command.result <- snapshotResult{err: err}
			return nil
		}
		direction, err := item.ship.Direction()
		if err != nil {
			command.result <- snapshotResult{err: err}
			return nil
		}
		state.Ships = append(state.Ships, shipState{
			ObjectID: item.id, OwnerID: item.owner,
			Position:  point{X: position.X, Y: position.Y},
			Velocity:  point{X: velocity.X, Y: velocity.Y},
			Direction: direction.Index(),
		})
	}
	command.result <- snapshotResult{state: state}
	return nil
}

type demo struct {
	handler  http.Handler
	gameID   string
	worker   *spacebattle.CommandWorker
	ships    []demoShip
	verifier *auth.TokenVerifier
}

func newDemo(users *auth.PasswordStore, privateKey ed25519.PrivateKey) (*demo, error) {
	if users == nil || !users.Exists("alice") || !users.Exists("bob") {
		return nil, errors.New("demo requires alice and bob accounts")
	}
	issuer, err := auth.NewTokenIssuer(privateKey, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	verifier, err := auth.NewTokenVerifier(privateKey.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, err
	}
	registry := auth.NewRegistry()
	gameID, err := registry.CreateGame([]string{"alice", "bob"})
	if err != nil {
		return nil, err
	}
	authService, err := auth.NewService(users, registry, issuer)
	if err != nil {
		return nil, err
	}
	direction, err := spacebattle.NewDirection(0, 4)
	if err != nil {
		return nil, err
	}
	alice, err := spacebattle.NewShip(spacebattle.Vector{}, 0, direction, 0)
	if err != nil {
		return nil, err
	}
	bob, err := spacebattle.NewShip(spacebattle.Vector{X: 10}, 0, direction, 0)
	if err != nil {
		return nil, err
	}
	ioc := spacebattle.NewIoC()
	operations, err := spacebattle.RegisterShipOrderOperations(ioc, nil)
	if err != nil {
		return nil, err
	}
	delete(operations, "Shoot") // This demo has no weapon policy.
	registration, err := ioc.Resolve[spacebattle.Command](nil, "IoC.Register", "game.order.advance", spacebattle.Factory(advanceFactory))
	if err != nil {
		return nil, err
	}
	if err := registration.Execute(); err != nil {
		return nil, err
	}
	operations["Advance"] = "game.order.advance"
	worker := spacebattle.NewCommandWorker()
	game, err := spacebattle.NewGame(spacebattle.GameConfig{
		Worker: worker, IoC: ioc,
		Objects:    map[string]any{"ship-alice": alice, "ship-bob": bob},
		Operations: operations,
	})
	if err != nil {
		return nil, err
	}
	commands, err := spacebattle.NewAuthorizedGameEndpoint(
		map[string]*spacebattle.Game{gameID: game}, verifier,
		map[string]map[string]string{gameID: {"ship-alice": "alice", "ship-bob": "bob"}},
	)
	if err != nil {
		return nil, err
	}
	current := &demo{
		gameID: gameID, worker: worker, verifier: verifier,
		ships: []demoShip{{"ship-alice", "alice", alice}, {"ship-bob", "bob", bob}},
	}
	mux := http.NewServeMux()
	mux.Handle("/games/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/games/"+gameID+"/tokens" {
			http.NotFound(w, r)
			return
		}
		authService.ServeHTTP(w, r)
	}))
	mux.Handle("/commands", commands)
	mux.HandleFunc("/state", current.serveState)
	current.handler = mux
	if err := worker.Start(); err != nil {
		return nil, err
	}
	return current, nil
}

func advanceFactory(args ...any) (any, error) {
	if len(args) != 2 {
		return nil, spacebattle.ErrInvalidArguments
	}
	object, ok := args[0].(spacebattle.MovingObject)
	if !ok || object == nil {
		return nil, spacebattle.ErrInvalidArguments
	}
	raw, ok := args[1].(json.RawMessage)
	if !ok {
		return nil, spacebattle.ErrInvalidArguments
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload struct{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, spacebattle.ErrInvalidArguments
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, spacebattle.ErrInvalidArguments
	}
	return spacebattle.Move{Object: object}, nil
}

func (current *demo) serveState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Query().Get("game_id") != current.gameID {
		http.NotFound(w, r)
		return
	}
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
	if _, err := current.verifier.Verify(parts[1], current.gameID); err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "invalid game token", http.StatusUnauthorized)
		return
	}
	result := make(chan snapshotResult, 1)
	if err := current.worker.Enqueue(snapshotCommand{gameID: current.gameID, ships: current.ships, result: result}); err != nil {
		http.Error(w, "game worker is unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	select {
	case snapshot := <-result:
		if snapshot.err != nil {
			http.Error(w, "could not read game state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(snapshot.state)
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "game state timed out", http.StatusGatewayTimeout)
		}
		return
	case <-current.worker.Done():
		http.Error(w, "game worker stopped", http.StatusServiceUnavailable)
	}
}

func (current *demo) close() {
	if current == nil || current.worker == nil {
		return
	}
	if err := current.worker.SoftStop(); err == nil {
		<-current.worker.Done()
	}
}

func randomPassword() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("battle-demo", flag.ContinueOnError)
	address := flags.String("listen", "127.0.0.1:8096", "loopback listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("battle-demo only listens on loopback")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	passwords := make(map[string]string, 2)
	hashes := make(map[string]string, 2)
	for _, userID := range []string{"alice", "bob"} {
		password, err := randomPassword()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		passwords[userID], hashes[userID] = password, hash
	}
	users, err := auth.NewPasswordStore(hashes)
	if err != nil {
		return err
	}
	current, err := newDemo(users, privateKey)
	if err != nil {
		return err
	}
	defer current.close()
	server := &http.Server{
		Addr: *address, Handler: current.handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute,
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	log.Printf("local game %s on http://%s", current.gameID, listener.Addr().String())
	for _, userID := range []string{"alice", "bob"} {
		log.Printf("temporary demo credentials: %s / %s", userID, passwords[userID])
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

var _ spacebattle.Command = snapshotCommand{}
