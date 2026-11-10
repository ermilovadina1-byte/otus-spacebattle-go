package spacebattle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func registerMoveFactory(t *testing.T, ioc *IoC) {
	t.Helper()
	factory := Factory(func(args ...any) (any, error) {
		if len(args) != 2 {
			return nil, ErrInvalidArguments
		}
		object, ok := args[0].(MovingObject)
		if !ok {
			return nil, ErrInvalidArguments
		}
		var options struct{}
		if err := json.Unmarshal(args[1].(json.RawMessage), &options); err != nil {
			return nil, err
		}
		return Move{Object: object}, nil
	})
	registration, err := ioc.Resolve[Command](nil, "IoC.Register", "game.move", factory)
	if err != nil {
		t.Fatal(err)
	}
	if err := registration.Execute(); err != nil {
		t.Fatal(err)
	}
}

func newMoveGame(t *testing.T, object *Torpedo) (*Game, *CommandWorker) {
	t.Helper()
	ioc := NewIoC()
	registerMoveFactory(t, ioc)
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{
		Worker: worker, IoC: ioc,
		Objects:    map[string]any{"ship-1": object},
		Operations: map[string]string{"move": "game.move"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	return game, worker
}

func postAgentMessage(endpoint *GameEndpoint, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/commands", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	endpoint.ServeHTTP(response, request)
	return response
}

func drainWorker(t *testing.T, worker *CommandWorker) {
	t.Helper()
	if err := worker.SoftStop(); err != nil {
		t.Fatal(err)
	}
	<-worker.Done()
}

func TestGameEndpointRoutesThroughInterpretAndExistingMove(t *testing.T) {
	first, _ := NewTorpedo(Vector{X: 1}, Vector{X: 2})
	second, _ := NewTorpedo(Vector{X: 10}, Vector{X: 3})
	firstGame, firstWorker := newMoveGame(t, first)
	secondGame, secondWorker := newMoveGame(t, second)
	endpoint, err := NewGameEndpoint(map[string]*Game{"game-a": firstGame, "game-b": secondGame})
	if err != nil {
		t.Fatal(err)
	}
	for _, gameID := range []string{"game-a", "game-b"} {
		body := fmt.Appendf(nil, `{"game_id":%q,"object_id":"ship-1","operation_id":"move","args":{}}`, gameID)
		if response := postAgentMessage(endpoint, body); response.Code != http.StatusAccepted {
			t.Fatalf("game %s: status=%d, body=%s", gameID, response.Code, response.Body.String())
		}
	}
	drainWorker(t, firstWorker)
	drainWorker(t, secondWorker)
	firstPosition, _ := first.Position()
	secondPosition, _ := second.Position()
	if firstPosition != (Vector{X: 3}) || secondPosition != (Vector{X: 13}) {
		t.Fatalf("routed positions: first=%+v, second=%+v", firstPosition, secondPosition)
	}
	if len(firstWorker.Failures()) != 0 || len(secondWorker.Failures()) != 0 {
		t.Fatalf("unexpected failures: first=%v, second=%v", firstWorker.Failures(), secondWorker.Failures())
	}
}

func TestGameEndpointRejectsBadTransportMessages(t *testing.T) {
	object, _ := NewTorpedo(Vector{}, Vector{X: 1})
	game, worker := newMoveGame(t, object)
	defer func() { drainWorker(t, worker) }()
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	valid := []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move","args":{}}`)
	for _, test := range []struct {
		name string
		body []byte
		want int
	}{
		{"malformed", []byte(`{"game_id":`), http.StatusBadRequest},
		{"missing ID", []byte(`{"game_id":"game","object_id":"","operation_id":"move","args":{}}`), http.StatusBadRequest},
		{"nonobject args", []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move","args":[]}`), http.StatusBadRequest},
		{"missing args", []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move"}`), http.StatusBadRequest},
		{"unknown field", []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move","args":{},"extra":1}`), http.StatusBadRequest},
		{"multiple values", append(bytes.Clone(valid), []byte(` {}`)...), http.StatusBadRequest},
		{"unknown game", []byte(`{"game_id":"absent","object_id":"ship-1","operation_id":"move","args":{}}`), http.StatusNotFound},
		{"too large", append(bytes.Clone(valid), bytes.Repeat([]byte(" "), maxAgentMessageBytes)...), http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := postAgentMessage(endpoint, test.body)
			if response.Code != test.want {
				t.Fatalf("status=%d, want=%d, body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/commands", nil)
	response := httptest.NewRecorder()
	endpoint.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET: status=%d, Allow=%q", response.Code, response.Header().Get("Allow"))
	}
	request = httptest.NewRequest(http.MethodPost, "/commands", bytes.NewReader(valid))
	response = httptest.NewRecorder()
	endpoint.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing Content-Type: %d", response.Code)
	}
}

func TestInterpretCommandUsesAllowlistAndReportsFailures(t *testing.T) {
	object, _ := NewTorpedo(Vector{}, Vector{X: 1})
	game, worker := newMoveGame(t, object)
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	for _, body := range []string{
		`{"game_id":"game","object_id":"ship-1","operation_id":"IoC.Register","args":{}}`,
		`{"game_id":"game","object_id":"missing","operation_id":"move","args":{}}`,
	} {
		if response := postAgentMessage(endpoint, []byte(body)); response.Code != http.StatusAccepted {
			t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
		}
	}
	drainWorker(t, worker)
	failures := worker.Failures()
	if len(failures) != 2 || !errors.Is(failures[0].Err, ErrUnknownOperation) || !errors.Is(failures[1].Err, ErrUnknownObject) {
		t.Fatalf("unexpected interpretation failures: %+v", failures)
	}
	position, _ := object.Position()
	if position != (Vector{}) {
		t.Fatalf("unauthorized message changed object: %+v", position)
	}
}

func TestGameEndpointConcurrentAgentsAndClosedWorker(t *testing.T) {
	object, _ := NewTorpedo(Vector{}, Vector{X: 1})
	game, worker := newMoveGame(t, object)
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	const requests = 80
	var group sync.WaitGroup
	errorsCh := make(chan error, requests)
	for range requests {
		group.Add(1)
		go func() {
			defer group.Done()
			response := postAgentMessage(endpoint, []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move","args":{}}`))
			if response.Code != http.StatusAccepted {
				errorsCh <- fmt.Errorf("status %d: %s", response.Code, response.Body.String())
			}
		}()
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	drainWorker(t, worker)
	position, _ := object.Position()
	if position != (Vector{X: requests}) || len(worker.Failures()) != 0 {
		t.Fatalf("position=%+v, failures=%v", position, worker.Failures())
	}
	response := postAgentMessage(endpoint, []byte(`{"game_id":"game","object_id":"ship-1","operation_id":"move","args":{}}`))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), ErrWorkerClosed.Error()) {
		t.Fatalf("closed worker: status=%d, body=%s", response.Code, response.Body.String())
	}
}

func TestGameEndpointCanAddOperationByConfiguration(t *testing.T) {
	type counter struct{ value int }
	object := &counter{}
	ioc := NewIoC()
	factory := Factory(func(args ...any) (any, error) {
		if len(args) != 2 || args[0] != object {
			return nil, ErrInvalidArguments
		}
		var payload struct {
			Amount int `json:"amount"`
		}
		if err := json.Unmarshal(args[1].(json.RawMessage), &payload); err != nil {
			return nil, err
		}
		return inboundTestCommand(func() error { object.value += payload.Amount; return nil }), nil
	})
	registration, _ := ioc.Resolve[Command](nil, "IoC.Register", "game.add", factory)
	if err := registration.Execute(); err != nil {
		t.Fatal(err)
	}
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{Worker: worker, IoC: ioc, Objects: map[string]any{"counter": object}, Operations: map[string]string{"add": "game.add"}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	response := postAgentMessage(endpoint, []byte(`{"game_id":"game","object_id":"counter","operation_id":"add","args":{"amount":7}}`))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
	}
	drainWorker(t, worker)
	if object.value != 7 || len(worker.Failures()) != 0 {
		t.Fatalf("value=%d, failures=%v", object.value, worker.Failures())
	}
}

func TestGameEndpointExecutesEachActionBeforeInterpretingNextMessage(t *testing.T) {
	type recorder struct{ events []string }
	object := &recorder{}
	ioc := NewIoC()
	factory := Factory(func(args ...any) (any, error) {
		if len(args) != 2 || args[0] != object {
			return nil, ErrInvalidArguments
		}
		var payload struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(args[1].(json.RawMessage), &payload); err != nil {
			return nil, err
		}
		object.events = append(object.events, "interpret "+payload.Label)
		return inboundTestCommand(func() error {
			object.events = append(object.events, "execute "+payload.Label)
			return nil
		}), nil
	})
	registration, _ := ioc.Resolve[Command](nil, "IoC.Register", "game.record", factory)
	if err := registration.Execute(); err != nil {
		t.Fatal(err)
	}
	worker := NewCommandWorker()
	game, err := NewGame(GameConfig{Worker: worker, IoC: ioc, Objects: map[string]any{"recorder": object}, Operations: map[string]string{"record": "game.record"}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := NewGameEndpoint(map[string]*Game{"game": game})
	for _, label := range []string{"A", "B"} {
		body := fmt.Appendf(nil, `{"game_id":"game","object_id":"recorder","operation_id":"record","args":{"label":%q}}`, label)
		if response := postAgentMessage(endpoint, body); response.Code != http.StatusAccepted {
			t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
		}
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	drainWorker(t, worker)
	want := []string{"interpret A", "execute A", "interpret B", "execute B"}
	if !slices.Equal(object.events, want) {
		t.Fatalf("events=%v, want=%v", object.events, want)
	}
}

type inboundTestCommand func() error

func (command inboundTestCommand) Execute() error { return command() }
