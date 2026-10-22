package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
	db "github.com/skydive-project/skydive/netdive/database"
)

type moldIntegrationHealth struct {
	State         string `json:"state"`
	CheckedAt     int64  `json:"checkedAt"`
	LastSuccessAt int64  `json:"lastSuccessAt,omitempty"`
}

type moldCommandObservation struct {
	sequence uint64
	failures int
	state    string
}

// Observes real requests only: no health-check HTTP calls, timers or log parsing.
// Per-command state prevents a successful unrelated API from clearing an error.
type moldIntegrationObserver struct {
	mu                sync.RWMutex
	health            moldIntegrationHealth
	readKeys          func() (string, string, error)
	sink              func(db.ChangeEvent)
	commands          map[string]moldCommandObservation
	credentialVersion [32]byte
	sequence          uint64
}

var moldIntegrationObserverRegistry struct {
	sync.RWMutex
	observer *moldIntegrationObserver
}

func newMoldIntegrationObserver(sink func(db.ChangeEvent)) *moldIntegrationObserver {
	return &moldIntegrationObserver{health: moldIntegrationHealth{State: "UNKNOWN"},
		readKeys: common.ReadMoldAPIKeys, sink: sink, commands: make(map[string]moldCommandObservation)}
}

func (m *moldIntegrationObserver) snapshot() moldIntegrationHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.health
}

// Call with mu held. Persist transitions, never API keys, signed URLs or bodies.
func (m *moldIntegrationObserver) publish(state string) {
	previous := m.health.State
	m.health.State, m.health.CheckedAt = state, time.Now().Unix()
	if state == previous || (previous == "UNKNOWN" && state == "HEALTHY") || m.sink == nil {
		return
	}
	severity := ""
	if state != "HEALTHY" {
		severity = "problem"
	}
	m.sink(db.ChangeEvent{ResourceType: "integration", ResourceID: "mold-api", ResourceName: "Mold API",
		EventType: "integration_state_changed", OldValue: previous, NewValue: state, Source: "mold",
		Severity: severity, Metadata: "{}", OccurredAt: m.health.CheckedAt})
}

func moldCredentialVersion(apiKey, secretKey string) [32]byte {
	return sha256.Sum256([]byte(apiKey + "\x00" + secretKey))
}

func (m *moldIntegrationObserver) setVersion(apiKey, secretKey string) {
	version := moldCredentialVersion(apiKey, secretKey)
	if version != m.credentialVersion {
		m.credentialVersion = version
		m.commands = make(map[string]moldCommandObservation)
	}
}

func (m *moldIntegrationObserver) aggregate() string {
	// A permission failure for one command must survive success of another.
	for _, state := range []string{"AUTHENTICATION_FAILED", "UNCONFIGURED", "PERMISSION_DENIED", "UNAVAILABLE"} {
		for _, entry := range m.commands {
			if entry.state == state {
				return state
			}
		}
	}
	for _, entry := range m.commands {
		if entry.state == "HEALTHY" {
			return "HEALTHY"
		}
	}
	return "UNKNOWN"
}

func (m *moldIntegrationObserver) record(command, state string, sequence uint64) {
	entry := m.commands[command]
	if sequence < entry.sequence {
		return
	}
	entry.sequence = sequence
	if state == "UNAVAILABLE" {
		entry.failures++
		// Ignore a single transient failure. Count existing requests; do not retry.
		if entry.failures >= 3 {
			entry.state = state
		}
	} else {
		entry.failures, entry.state = 0, state
	}
	m.commands[command] = entry
	if state == "HEALTHY" {
		m.health.LastSuccessAt = time.Now().Unix()
	}
	m.publish(m.aggregate())
}

func (m *moldIntegrationObserver) begin(command, apiKey, secretKey string) func(int, []byte, error) {
	m.mu.Lock()
	m.sequence++
	sequence := m.sequence
	m.mu.Unlock()
	return func(status int, body []byte, err error) {
		// Ignore tests of unsaved input and responses for a pair replaced in flight.
		currentAPI, currentSecret, currentErr := m.readKeys()
		if currentErr != nil || currentAPI != apiKey || currentSecret != secretKey {
			return
		}
		state := moldAPIObservationState(status, body, err)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.setVersion(apiKey, secretKey)
		delete(m.commands, "credentials")
		if state == "" {
			// A resource/operation error interrupts consecutive transport failures,
			// but does not establish a successful data retrieval or clear a verdict.
			entry := m.commands[strings.ToLower(command)]
			if sequence >= entry.sequence {
				entry.sequence, entry.failures = sequence, 0
				m.commands[strings.ToLower(command)] = entry
			}
			return
		}
		m.record(strings.ToLower(command), state, sequence)
	}
}

func (m *moldIntegrationObserver) credentialError() {
	_, _, err := m.readKeys()
	if err == nil {
		return
	}
	state := "UNAVAILABLE"
	if errors.Is(err, common.ErrMoldAPICredentialsNotConfigured) {
		state = "UNCONFIGURED"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if state == "UNCONFIGURED" {
		m.commands = make(map[string]moldCommandObservation)
		m.credentialVersion = [32]byte{}
	}
	m.sequence++
	m.record("credentials", state, m.sequence)
}

// Saving a new pair already performed the existing API connection test. Use its
// validated result instead of issuing another request after saving.
func (m *moldIntegrationObserver) saved(apiKey, secretKey string) {
	currentAPI, currentSecret, err := m.readKeys()
	if err != nil || currentAPI != apiKey || currentSecret != secretKey {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setVersion(apiKey, secretKey)
	delete(m.commands, "credentials")
	m.sequence++
	m.record("listcapabilities", "HEALTHY", m.sequence)
}

func moldAPIObservationState(status int, body []byte, err error) string {
	if status == http.StatusUnauthorized {
		return "AUTHENTICATION_FAILED"
	}
	if status == http.StatusForbidden {
		return "PERMISSION_DENIED"
	}
	if status == 0 || status >= 500 {
		return "UNAVAILABLE"
	}
	if status < 200 || status >= 300 {
		return ""
	}
	// Skip normal payload fields instead of copying entire resource lists into
	// a second map just to inspect the small error envelope.
	var payload struct {
		Error json.RawMessage `json:"errorresponse"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) <= 2 || trimmed[0] != '{' || json.Unmarshal(body, &payload) != nil {
		return "UNAVAILABLE"
	}
	if len(payload.Error) > 0 {
		var apiError struct {
			Code int `json:"errorcode"`
		}
		if json.Unmarshal(payload.Error, &apiError) != nil {
			return "UNAVAILABLE"
		}
		switch apiError.Code {
		case http.StatusUnauthorized:
			return "AUTHENTICATION_FAILED"
		case http.StatusForbidden:
			return "PERMISSION_DENIED"
		default:
			if apiError.Code >= 500 {
				return "UNAVAILABLE"
			}
			return ""
		}
	}
	if err != nil {
		return "UNAVAILABLE"
	}
	return "HEALTHY" // Empty result arrays/count=0 are valid data.
}

func currentMoldIntegrationObserver() *moldIntegrationObserver {
	moldIntegrationObserverRegistry.RLock()
	defer moldIntegrationObserverRegistry.RUnlock()
	return moldIntegrationObserverRegistry.observer
}

func beginMoldAPIObservation(command, apiKey, secretKey string) func(int, []byte, error) {
	if observer := currentMoldIntegrationObserver(); observer != nil {
		return observer.begin(command, apiKey, secretKey)
	}
	return func(int, []byte, error) {}
}

func recordMoldCredentialError() {
	if observer := currentMoldIntegrationObserver(); observer != nil {
		observer.credentialError()
	}
}

func recordSavedMoldCredentials(apiKey, secretKey string) {
	if observer := currentMoldIntegrationObserver(); observer != nil {
		observer.saved(apiKey, secretKey)
	}
}

func (m *moldIntegrationObserver) handleGet(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, "topology", "read") {
		writeMoldCredentialsJSON(w, http.StatusForbidden, moldCredentialsStatus{Message: "조회 권한이 없습니다."})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, m.snapshot())
}

func RegisterMoldIntegrationHealthAPI(server *shttp.Server, backend shttp.AuthenticationBackend, database *db.Database) func() {
	observer := newMoldIntegrationObserver(database.RecordEvent)
	moldIntegrationObserverRegistry.Lock()
	moldIntegrationObserverRegistry.observer = observer
	moldIntegrationObserverRegistry.Unlock()
	server.RegisterRoutes([]shttp.Route{{Name: "MoldIntegrationHealth", Method: "GET", Path: "/api/mold/integration-health", HandlerFunc: observer.handleGet}}, backend)
	return func() {
		moldIntegrationObserverRegistry.Lock()
		if moldIntegrationObserverRegistry.observer == observer {
			moldIntegrationObserverRegistry.observer = nil
		}
		moldIntegrationObserverRegistry.Unlock()
	}
}
