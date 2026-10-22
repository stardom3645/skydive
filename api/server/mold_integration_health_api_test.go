package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	db "github.com/skydive-project/skydive/netdive/database"
)

const moldObservationSuccess = `{"listkubernetesclustersresponse":{"count":0,"kubernetescluster":[]}}`

func moldObservationFixture() (*moldIntegrationObserver, *[]db.ChangeEvent) {
	events := []db.ChangeEvent{}
	m := newMoldIntegrationObserver(func(e db.ChangeEvent) { events = append(events, e) })
	m.readKeys = func() (string, string, error) { return "test-api-key", "test-secret", nil }
	return m, &events
}

func observeMoldTest(m *moldIntegrationObserver, command string, status int) {
	m.begin(command, "test-api-key", "test-secret")(status, []byte(moldObservationSuccess), nil)
}

func TestMoldIntegrationRealRequestTransitions(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listKubernetesClusters", 401)
	for i := 0; i < 10; i++ {
		observeMoldTest(m, "listKubernetesClusters", 401)
	}
	if len(*events) != 1 || (*events)[0].OldValue != "UNKNOWN" || (*events)[0].Severity != "problem" {
		t.Fatalf("initial/repeated failure events: %+v", *events)
	}
	observeMoldTest(m, "listKubernetesClusters", 403)
	observeMoldTest(m, "listKubernetesClusters", 200)
	observeMoldTest(m, "listKubernetesClusters", 200)
	if len(*events) != 3 || (*events)[2].NewValue != "HEALTHY" || (*events)[2].Severity != "" || m.snapshot().LastSuccessAt == 0 {
		t.Fatalf("recovery events: %+v", *events)
	}
	if (*events)[0].ResourceID != "mold-api" || (*events)[0].EventType != "integration_state_changed" || (*events)[0].Metadata != "{}" {
		t.Fatalf("invalid event identity: %+v", (*events)[0])
	}
}

func TestMoldIntegrationEmptyDataIsHealthy(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listKubernetesClusters", 200)
	if m.snapshot().State != "HEALTHY" || len(*events) != 0 {
		t.Fatalf("empty data caused an alarm: %+v", *events)
	}
}

func TestMoldIntegrationTransportFailureThresholdAndRecovery(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listHosts", 200)
	m.health.LastSuccessAt = 123 // Failed attempts must not move the success timestamp.
	for i := 0; i < 2; i++ {
		observeMoldTest(m, "listHosts", 502)
	}
	if m.snapshot().State != "HEALTHY" || len(*events) != 0 || m.snapshot().LastSuccessAt != 123 {
		t.Fatal("transient errors changed the verdict or success time")
	}
	observeMoldTest(m, "listHosts", 502)
	observeMoldTest(m, "listHosts", 502)
	if m.snapshot().State != "UNAVAILABLE" || len(*events) != 1 {
		t.Fatalf("sustained failure missing or duplicated: %+v", *events)
	}
	observeMoldTest(m, "listHosts", 200)
	if m.snapshot().State != "HEALTHY" || len(*events) != 2 {
		t.Fatalf("recovery missing: %+v", *events)
	}
}

func TestMoldIntegrationUnrelatedCommandsDoNotFlap(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listKubernetesClusters", 403)
	for i := 0; i < 10; i++ {
		observeMoldTest(m, "listCapabilities", 200)
		observeMoldTest(m, "listHosts", 200)
	}
	if m.snapshot().State != "PERMISSION_DENIED" || len(*events) != 1 {
		t.Fatalf("unrelated success cleared failure: %+v", *events)
	}
	observeMoldTest(m, "listKubernetesClusters", 200)
	if m.snapshot().State != "HEALTHY" || len(*events) != 2 {
		t.Fatal("successful retry did not recover")
	}
}

func TestMoldIntegrationIgnoredOperationBreaksConsecutiveFailures(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listHosts", 200)
	observeMoldTest(m, "listHosts", 502)
	observeMoldTest(m, "listHosts", 502)
	observeMoldTest(m, "listHosts", 404)
	observeMoldTest(m, "listHosts", 502)
	if len(*events) != 0 {
		t.Fatal("operation failure was counted as consecutive connection failure")
	}
}

func TestMoldIntegrationMissingCredentials(t *testing.T) {
	m, events := moldObservationFixture()
	m.readKeys = func() (string, string, error) { return "", "", common.ErrMoldAPICredentialsNotConfigured }
	m.credentialError()
	m.credentialError()
	if m.snapshot().State != "UNCONFIGURED" || len(*events) != 1 {
		t.Fatalf("missing credentials not reported once: %+v", *events)
	}
	m.readKeys = func() (string, string, error) { return "test-api-key", "test-secret", nil }
	observeMoldTest(m, "listHosts", 200)
	if m.snapshot().State != "HEALTHY" || len(*events) != 2 {
		t.Fatal("credential configuration did not recover")
	}
}

func TestMoldIntegrationCredentialStorageFailureIsNotMissingKey(t *testing.T) {
	m, _ := moldObservationFixture()
	m.readKeys = func() (string, string, error) { return "", "", errors.New("storage failure") }
	for i := 0; i < 3; i++ {
		m.credentialError()
	}
	if m.snapshot().State != "UNAVAILABLE" {
		t.Fatal("storage error classified as missing or invalid credentials")
	}
}

func TestMoldIntegrationIgnoresUnsavedRotatedAndOutOfOrderResponses(t *testing.T) {
	m, events := moldObservationFixture()
	m.begin("listHosts", "unsaved-key", "unsaved-secret")(401, nil, nil)
	if len(*events) != 0 {
		t.Fatal("unsaved input affected stored-key health")
	}
	old := m.begin("listHosts", "test-api-key", "test-secret")
	observeMoldTest(m, "listHosts", 200)
	old(401, nil, nil)
	if m.snapshot().State != "HEALTHY" || len(*events) != 0 {
		t.Fatal("late response overrode newer success")
	}
	old = m.begin("listHosts", "test-api-key", "test-secret")
	m.readKeys = func() (string, string, error) { return "new-example-key", "new-example-secret", nil }
	old(401, nil, nil)
	if len(*events) != 0 {
		t.Fatal("old credential pair result was published")
	}
	encoded, _ := json.Marshal(m.snapshot())
	if strings.Contains(string(encoded), "example") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("health leaked credentials: %s", encoded)
	}
}

func TestMoldIntegrationSavedValidatedPairRecoversWithoutProbe(t *testing.T) {
	m, events := moldObservationFixture()
	observeMoldTest(m, "listHosts", 401)
	m.saved("new-example-key", "new-example-secret")
	if len(*events) != 1 {
		t.Fatal("uncommitted key affected status")
	}
	m.readKeys = func() (string, string, error) { return "new-example-key", "new-example-secret", nil }
	m.saved("new-example-key", "new-example-secret")
	if m.snapshot().State != "HEALTHY" || len(*events) != 2 {
		t.Fatalf("validated save did not recover: %+v", *events)
	}
}

func TestMoldIntegrationObservationClassification(t *testing.T) {
	for _, test := range []struct {
		name, body, state string
		status            int
	}{
		{"empty result", moldObservationSuccess, "HEALTHY", 200},
		{"invalid key", "", "AUTHENTICATION_FAILED", 401},
		{"permission denied", "", "PERMISSION_DENIED", 403},
		{"network error", "", "UNAVAILABLE", 0},
		{"server unavailable", "", "UNAVAILABLE", 503},
		{"invalid JSON", "<html>login</html>", "UNAVAILABLE", 200},
		{"JSON authentication error", `{"errorresponse":{"errorcode":401}}`, "AUTHENTICATION_FAILED", 200},
		{"JSON permission error", `{"errorresponse":{"errorcode":403}}`, "PERMISSION_DENIED", 200},
		{"JSON server error", `{"errorresponse":{"errorcode":500}}`, "UNAVAILABLE", 200},
		{"missing resource", "", "", 404},
		{"bad operation", `{"errorresponse":{"errorcode":431}}`, "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := moldAPIObservationState(test.status, []byte(test.body), nil); got != test.state {
				t.Fatalf("state=%s, expected %s", got, test.state)
			}
		})
	}
}

func TestMoldIntegrationHealthAndPersistentEventAPI(t *testing.T) {
	fixture := newManualPortMappingAPIFixture(t)
	m := newMoldIntegrationObserver(fixture.db.RecordEvent)
	m.readKeys = func() (string, string, error) { return "test-api-key", "test-secret", nil }
	observeMoldTest(m, "listHosts", 401)
	observeMoldTest(m, "listHosts", 401)
	observeMoldTest(m, "listHosts", 200)
	m.readKeys = func() (string, string, error) {
		t.Fatal("health GET must not read credentials or call Mold")
		return "", "", nil
	}
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		m.handleGet(w, authenticatedManualPortMappingRequest(http.MethodGet, "/api/mold/integration-health", nil))
		var health moldIntegrationHealth
		if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil || w.Code != 200 || health.State != "HEALTHY" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("health API: %d %s, %v", w.Code, w.Body.String(), err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		w := httptest.NewRecorder()
		eventHistoryHandler(fixture.db)(w, authenticatedManualPortMappingRequest(http.MethodGet, "/api/events?resource_type=integration&resource_id=mold-api&source=mold", nil))
		var page db.EventPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 {
			t.Fatalf("event API: %d %s, %v", w.Code, w.Body.String(), err)
		}
		if page.Total == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 persisted events, got %d", page.Total)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMoldIntegrationExistingHTTPCallsHaveNoExtraRequests(t *testing.T) {
	m, events := moldObservationFixture()
	moldIntegrationObserverRegistry.Lock()
	previousObserver := moldIntegrationObserverRegistry.observer
	moldIntegrationObserverRegistry.observer = m
	moldIntegrationObserverRegistry.Unlock()
	previousEndpoint := config.GetString("mold.api.endpoint")
	common.SetMoldAPICredentialsStore(kubernetesListTestCredentials{})
	t.Cleanup(func() {
		moldIntegrationObserverRegistry.Lock()
		moldIntegrationObserverRegistry.observer = previousObserver
		moldIntegrationObserverRegistry.Unlock()
		config.Set("mold.api.endpoint", previousEndpoint)
		common.SetMoldAPICredentialsStore(nil)
	})
	requests := 0
	status := 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(moldObservationSuccess))
	}))
	defer server.Close()
	config.Set("mold.api.endpoint", server.URL)
	_, _, _ = requestMoldAPI("listHosts", []apiParam{{Key: "command", Value: "listHosts"}, {Key: "response", Value: "json"}})
	if requests != 1 || m.snapshot().State != "AUTHENTICATION_FAILED" || len(*events) != 1 {
		t.Fatalf("requests=%d, health=%+v, events=%+v", requests, m.snapshot(), *events)
	}
	status = 200
	_, _, _ = requestMoldAPI("listHosts", []apiParam{{Key: "command", Value: "listHosts"}, {Key: "response", Value: "json"}})
	if requests != 2 || m.snapshot().State != "HEALTHY" || len(*events) != 2 {
		t.Fatal("existing successful retry was not observed")
	}
	// Existing console verification is observed too; no second health request.
	baseURL := server.URL
	parsed, _ := http.NewRequest(http.MethodGet, baseURL, nil)
	_, _, _ = callMoldAPI(server.Client(), parsed.URL, "listCapabilities", "test-api-key", "test-secret", []apiParam{{Key: "command", Value: "listCapabilities"}})
	if requests != 3 {
		t.Fatalf("console observation made extra requests: %d", requests)
	}
}
