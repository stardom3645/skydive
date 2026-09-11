package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/skydive-project/skydive/netdive/database"
	"github.com/skydive-project/skydive/netdive/eventhistory"
)

func TestEventHistoryAPIValidationAndFiltering(t *testing.T) {
	f := newManualPortMappingAPIFixture(t)
	_, err := f.db.SQLDB().Exec(`INSERT INTO event_history(resource_type,resource_id,resource_name,event_type,source,occurred_at) VALUES(?,?,?,?,?,?)`, "nic", "stable", "eno1", "link_changed", "infrastructure", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"?pageSize=0", "?page=-1", "?pageSize=101", "?from=no", "?from=20&to=10", "?from=1&to=999999999999"} {
		w := httptest.NewRecorder()
		eventHistoryHandler(f.db)(w, authenticatedManualPortMappingRequest("GET", "/api/events"+query, nil))
		if w.Code != 400 {
			t.Fatalf("%s returned %d: %s", query, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	eventHistoryHandler(f.db)(w, authenticatedManualPortMappingRequest("GET", "/api/events?resource_id=stable&source=infrastructure&search=eno", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var page db.EventPage
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 || page.PageSize != 20 {
		t.Fatal(page, err)
	}
	w = httptest.NewRecorder()
	eventHistoryHandler(nil)(w, authenticatedManualPortMappingRequest("GET", "/api/events", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestEventCollectionLifecycleIgnoresIntermediateSync(t *testing.T) {
	events := []db.ChangeEvent{}
	recorder := eventhistory.New(func(e db.ChangeEvent) { events = append(events, e) })
	historyObserver.Lock()
	previous := historyObserver.recorder
	historyObserver.recorder = recorder
	historyObserver.Unlock()
	defer func() {
		historyObserver.Lock()
		historyObserver.recorder = previous
		historyObserver.Unlock()
		recorder.Close()
	}()
	for _, status := range []kubernetesAPIConnectionStatus{kubernetesHealthy, kubernetesSyncing, kubernetesConnected, kubernetesHealthy, kubernetesPermissionDenied, kubernetesPermissionDenied, kubernetesHealthy} {
		setKubernetesCollectionState("event-test", status, nil)
	}
	if len(events) != 2 || events[0].NewValue != "PERMISSION_DENIED" || events[1].NewValue != "HEALTHY" {
		t.Fatal(events)
	}
	kubernetesClientRegistry.Lock()
	delete(kubernetesClientRegistry.states, "event-test")
	kubernetesClientRegistry.Unlock()
}
