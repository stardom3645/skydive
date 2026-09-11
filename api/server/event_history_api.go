package server

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/skydive-project/skydive/graffiti/graph"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
	db "github.com/skydive-project/skydive/netdive/database"
	"github.com/skydive-project/skydive/netdive/eventhistory"
)

var historyObserver struct {
	sync.RWMutex
	recorder *eventhistory.Recorder
}

func observeLifecycle(e db.ChangeEvent) {
	historyObserver.RLock()
	defer historyObserver.RUnlock()
	if historyObserver.recorder != nil {
		historyObserver.recorder.Observe(e)
	}
}

// Registration uses the existing authenticated topology read permission.
func RegisterEventHistoryAPI(server *shttp.Server, backend shttp.AuthenticationBackend, database *db.Database, g *graph.Graph) func() {
	var recorder *eventhistory.Recorder
	if database != nil {
		recorder = eventhistory.Attach(g, database)
	}
	historyObserver.Lock()
	historyObserver.recorder = recorder
	historyObserver.Unlock()
	server.RegisterRoutes([]shttp.Route{{Name: "EventHistory", Method: "GET", Path: "/api/events", HandlerFunc: eventHistoryHandler(database)}}, backend)
	return func() {
		historyObserver.Lock()
		if historyObserver.recorder == recorder {
			historyObserver.recorder = nil
		}
		historyObserver.Unlock()
		if recorder != nil {
			recorder.Close()
		}
	}
}

func eventHistoryHandler(database *db.Database) func(http.ResponseWriter, *auth.AuthenticatedRequest) {
	return func(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
		if !rbac.Enforce(r.Username, "topology", "read") {
			writeManualPortMappingError(w, 403, "조회 권한이 없습니다.")
			return
		}
		if database == nil {
			writeManualPortMappingError(w, 503, "Netdive 변경 이력 저장소가 설정되지 않았습니다.")
			return
		}
		q := r.URL.Query()
		now := time.Now().Unix()
		f := db.EventFilter{From: now - 86400, To: now, Page: 1, PageSize: 20, ResourceType: q.Get("resource_type"), ResourceID: q.Get("resource_id"), EventType: q.Get("event_type"), Source: q.Get("source"), Search: q.Get("search")}
		for _, p := range []struct {
			key   string
			value *int64
		}{{"from", &f.From}, {"to", &f.To}} {
			if raw := q.Get(p.key); raw != "" {
				v, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || v < 0 {
					writeManualPortMappingError(w, 400, "기간 값이 올바르지 않습니다.")
					return
				}
				*p.value = v
			}
		}
		for _, p := range []struct {
			key   string
			value *int
			max   int
		}{{"page", &f.Page, 1000000}, {"pageSize", &f.PageSize, 100}} {
			if raw := q.Get(p.key); raw != "" {
				v, err := strconv.Atoi(raw)
				if err != nil || v < 1 || v > p.max {
					writeManualPortMappingError(w, 400, "페이지 값이 올바르지 않습니다.")
					return
				}
				*p.value = v
			}
		}
		if f.From > f.To || f.To-f.From > 366*86400 || len(f.Search) > 256 {
			writeManualPortMappingError(w, 400, "조회 범위를 확인하세요. 최대 366일입니다.")
			return
		}
		page, err := database.ListEvents(r.Context(), f)
		if err != nil {
			writeManualPortMappingError(w, 500, "변경 이력을 조회하지 못했습니다.")
			return
		}
		writeManualPortMappingJSON(w, 200, page)
	}
}
