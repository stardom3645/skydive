package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skydive-project/skydive/graffiti/logging"
)

// ChangeEvent stores only a transition, never a topology or metric snapshot.
// Severity is deliberately empty unless the source has a domain verdict.
type ChangeEvent struct {
	ID           int64  `json:"id"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	EventType    string `json:"eventType"`
	OldValue     string `json:"oldValue"`
	NewValue     string `json:"newValue"`
	Source       string `json:"source"`
	Severity     string `json:"severity,omitempty"`
	Metadata     string `json:"metadata"`
	OccurredAt   int64  `json:"occurredAt"`
}

type EventFilter struct {
	From, To                                            int64
	ResourceType, ResourceID, EventType, Source, Search string
	Page, PageSize                                      int
}

type EventPage struct {
	Events   []ChangeEvent `json:"events"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

func (d *Database) insertEvent(ctx context.Context, e ChangeEvent) error {
	if e.OccurredAt == 0 {
		e.OccurredAt = time.Now().Unix()
	}
	if e.Metadata == "" {
		e.Metadata = "{}"
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO event_history(resource_type,resource_id,resource_name,event_type,old_value,new_value,source,severity,metadata,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, e.ResourceType, e.ResourceID, e.ResourceName, e.EventType, e.OldValue, e.NewValue, e.Source, e.Severity, e.Metadata, e.OccurredAt)
	return err
}

func (d *Database) ListEvents(ctx context.Context, f EventFilter) (EventPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	if f.From == 0 {
		f.From = time.Now().Add(-24 * time.Hour).Unix()
	}
	if f.To == 0 {
		f.To = time.Now().Unix()
	}
	result := EventPage{Events: []ChangeEvent{}, Page: f.Page, PageSize: f.PageSize}
	where := []string{"occurred_at >= ?", "occurred_at <= ?"}
	args := []interface{}{f.From, f.To}
	for _, field := range []struct{ column, value string }{{"resource_type", f.ResourceType}, {"resource_id", f.ResourceID}, {"event_type", f.EventType}, {"source", f.Source}} {
		if field.value != "" {
			where = append(where, field.column+" = ?")
			args = append(args, field.value)
		}
	}
	if f.Search != "" {
		where = append(where, "instr(lower(resource_name), lower(?)) > 0")
		args = append(args, f.Search)
	}
	clause := " FROM event_history WHERE " + strings.Join(where, " AND ")
	// One read transaction gives the count and page the same WAL snapshot.
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*)"+clause, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := tx.QueryContext(ctx, "SELECT id,resource_type,resource_id,resource_name,event_type,old_value,new_value,source,severity,metadata,occurred_at"+clause+" ORDER BY occurred_at DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var e ChangeEvent
		if err = rows.Scan(&e.ID, &e.ResourceType, &e.ResourceID, &e.ResourceName, &e.EventType, &e.OldValue, &e.NewValue, &e.Source, &e.Severity, &e.Metadata, &e.OccurredAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Events = append(result.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// Delete in bounded transactions. A large backlog is gradually removed without
// holding a long write lock; no startup deletion, VACUUM or forced checkpoint.
func (d *Database) CleanupEvents(ctx context.Context, before int64) (int64, error) {
	result, err := d.db.ExecContext(ctx, `DELETE FROM event_history WHERE id IN (SELECT id FROM event_history WHERE occurred_at < ? ORDER BY occurred_at LIMIT 1000)`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type eventWriter struct {
	db        *Database
	queue     chan ChangeEvent
	done      chan struct{}
	mu        sync.RWMutex
	closed    bool
	dropped   uint64
	retention int
	failures  uint64
}

func newEventWriter(db *Database, days int) *eventWriter {
	if days <= 0 {
		days = 30
	}
	w := &eventWriter{db: db, queue: make(chan ChangeEvent, 1024), done: make(chan struct{}), retention: days}
	go w.run()
	return w
}

// RecordEvent never waits for SQLite, including when its writer is locked.
// Overload is explicitly best effort: bounded memory, throttled WARN logs.
func (d *Database) RecordEvent(e ChangeEvent) {
	if d == nil || d.events == nil {
		return
	}
	w := d.events
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return
	}
	if e.OccurredAt == 0 {
		e.OccurredAt = time.Now().Unix()
	}
	select {
	case w.queue <- e:
	default:
		n := atomic.AddUint64(&w.dropped, 1)
		if n == 1 || n%1000 == 0 {
			logging.GetLogger().Warningf("Netdive event queue full; dropped %d events", n)
		}
	}
}

func (w *eventWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-w.queue:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := w.db.insertEvent(ctx, e)
			cancel()
			if err != nil {
				w.failures++
				if w.failures == 1 || w.failures%100 == 0 {
					logging.GetLogger().Warningf("Netdive event history write failed (%d failures): %s", w.failures, err)
				}
			} else {
				w.failures = 0
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var n int64
			var err error
			for ctx.Err() == nil {
				var batch int64
				batch, err = w.db.CleanupEvents(ctx, time.Now().AddDate(0, 0, -w.retention).Unix())
				n += batch
				if err != nil || batch < 1000 {
					break
				}
			}
			cancel()
			if err != nil {
				logging.GetLogger().Warningf("Netdive event cleanup failed: %s", err)
			} else {
				logging.GetLogger().Debugf("Netdive event cleanup removed %d rows", n)
			}
		}
	}
}

func (w *eventWriter) close() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		logging.GetLogger().Warningf("Netdive event writer shutdown timed out")
	}
}

func manualEvent(m ManualPortMapping, kind, oldValue, newValue string) ChangeEvent {
	metadata, _ := json.Marshal(map[string]interface{}{"mappingId": m.ID, "portNodeId": m.SwitchPortNodeID, "switchNodeId": m.SwitchNodeID})
	return ChangeEvent{ResourceType: "switchport", ResourceID: fmt.Sprintf("manual-mapping:%d", m.ID), ResourceName: m.SwitchPortName, EventType: kind, OldValue: oldValue, NewValue: newValue, Source: "manual", Metadata: string(metadata)}
}

func manualMappingValue(m ManualPortMapping) string {
	value, _ := json.Marshal([]interface{}{m.SwitchNodeID, m.SwitchPortName, m.HostNICNodeID, m.Enabled})
	return string(value)
}
