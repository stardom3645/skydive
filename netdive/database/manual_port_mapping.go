// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// Repository-level errors allow the HTTP layer to return stable status codes
// without exposing SQLite-specific details.
var (
	ErrManualPortMappingNotFound = errors.New("manual port mapping not found")
	ErrManualPortMappingConflict = errors.New("manual port mapping conflicts with an active mapping")
	ErrManualPortMappingInactive = errors.New("inactive manual port mapping history cannot be modified")
)

// Manual mapping disable reasons are persisted so an AUTO takeover remains
// explainable after an analyzer restart. They are intentionally data values,
// not translated labels; API/UI callers may localize them independently.
const (
	ManualPortMappingDisabledByUser             = "user"
	ManualPortMappingDisabledByLLDPMatch        = "lldp_auto_match"
	ManualPortMappingDisabledByLLDPPortConflict = "lldp_auto_port_conflict"
	ManualPortMappingDisabledByLLDPNICConflict  = "lldp_auto_nic_conflict"
	ManualPortMappingDisabledByLLDPConflict     = "lldp_auto_conflict"
)

// ManualPortMapping is a persisted administrator-supplied physical relation.
// Switch/host/NIC node IDs refer to collected topology. SwitchPortName is the
// administrator's authoritative free-form value; SwitchPortNodeID is optional
// legacy context because an uncollected port has no topology node.
type ManualPortMapping struct {
	ID               int64  `json:"id"`
	SwitchNodeID     string `json:"switchNodeId"`
	SwitchName       string `json:"switchName,omitempty"`
	SwitchPortNodeID string `json:"switchPortNodeId,omitempty"`
	SwitchPortName   string `json:"switchPortName,omitempty"`
	HostNodeID       string `json:"hostNodeId"`
	HostName         string `json:"hostName,omitempty"`
	HostNICNodeID    string `json:"hostNicNodeId"`
	HostNICName      string `json:"hostNicName,omitempty"`
	Enabled          bool   `json:"enabled"`
	DisabledReason   string `json:"disabledReason,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

// ManualPortMappingFilter supports the switch and host/NIC lookup paths needed
// by detail panels without periodically writing topology state to SQLite.
type ManualPortMappingFilter struct {
	SwitchNodeID    string
	HostNodeID      string
	HostNICNodeID   string
	IncludeDisabled bool
}

// ListManualPortMappings returns mappings matching all supplied filters.
func (d *Database) ListManualPortMappings(ctx context.Context, filter ManualPortMappingFilter) ([]ManualPortMapping, error) {
	query := `SELECT id, switch_node_id, switch_name, switch_port_node_id, switch_port_name,
		host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, disabled_reason, created_at, updated_at
		FROM manual_port_mapping WHERE 1 = 1`
	args := make([]interface{}, 0, 4)
	if !filter.IncludeDisabled {
		query += " AND enabled = 1"
	}
	if filter.SwitchNodeID != "" {
		query += " AND switch_node_id = ?"
		args = append(args, filter.SwitchNodeID)
	}
	if filter.HostNodeID != "" {
		query += " AND host_node_id = ?"
		args = append(args, filter.HostNodeID)
	}
	if filter.HostNICNodeID != "" {
		query += " AND host_nic_node_id = ?"
		args = append(args, filter.HostNICNodeID)
	}
	query += " ORDER BY switch_name COLLATE NOCASE, switch_port_name COLLATE NOCASE, id"

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list manual port mappings: %w", err)
	}
	defer rows.Close()
	mappings := make([]ManualPortMapping, 0)
	for rows.Next() {
		mapping, err := scanManualPortMapping(rows)
		if err != nil {
			return nil, fmt.Errorf("scan manual port mapping: %w", err)
		}
		mappings = append(mappings, mapping)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list manual port mappings: %w", err)
	}
	return mappings, nil
}

// GetManualPortMapping returns one mapping by database ID.
func (d *Database) GetManualPortMapping(ctx context.Context, id int64) (ManualPortMapping, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, switch_node_id, switch_name, switch_port_node_id, switch_port_name,
		host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, disabled_reason, created_at, updated_at
		FROM manual_port_mapping WHERE id = ?`, id)
	mapping, err := scanManualPortMapping(row)
	if err == sql.ErrNoRows {
		return ManualPortMapping{}, ErrManualPortMappingNotFound
	}
	if err != nil {
		return ManualPortMapping{}, fmt.Errorf("get manual port mapping: %w", err)
	}
	return mapping, nil
}

// CreateManualPortMapping inserts a mapping. Database uniqueness constraints
// serialize concurrent attempts to assign one active port or NIC twice.
func (d *Database) CreateManualPortMapping(ctx context.Context, mapping ManualPortMapping) (ManualPortMapping, error) {
	result, err := d.db.ExecContext(ctx, `INSERT INTO manual_port_mapping (
		switch_node_id, switch_name, switch_port_node_id, switch_port_name,
		host_node_id, host_name, host_nic_node_id, host_nic_name, enabled
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mapping.SwitchNodeID, mapping.SwitchName, nullableText(mapping.SwitchPortNodeID), strings.TrimSpace(mapping.SwitchPortName),
		mapping.HostNodeID, mapping.HostName, mapping.HostNICNodeID, mapping.HostNICName, boolInt(mapping.Enabled))
	if err != nil {
		return ManualPortMapping{}, manualPortMappingWriteError("create", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ManualPortMapping{}, fmt.Errorf("read created manual port mapping ID: %w", err)
	}
	return d.GetManualPortMapping(ctx, id)
}

// UpdateManualPortMapping replaces the stable endpoints of an active mapping
// while preserving its creation timestamp. Disabled rows are immutable audit
// history and must never be reactivated in place after an AUTO takeover.
func (d *Database) UpdateManualPortMapping(ctx context.Context, mapping ManualPortMapping) (ManualPortMapping, error) {
	result, err := d.db.ExecContext(ctx, `UPDATE manual_port_mapping SET
		switch_node_id = ?, switch_name = ?, switch_port_node_id = ?, switch_port_name = ?,
		host_node_id = ?, host_name = ?, host_nic_node_id = ?, host_nic_name = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND enabled = 1`,
		mapping.SwitchNodeID, mapping.SwitchName, nullableText(mapping.SwitchPortNodeID), strings.TrimSpace(mapping.SwitchPortName),
		mapping.HostNodeID, mapping.HostName, mapping.HostNICNodeID, mapping.HostNICName, mapping.ID)
	if err != nil {
		return ManualPortMapping{}, manualPortMappingWriteError("update", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ManualPortMapping{}, fmt.Errorf("read updated manual port mapping count: %w", err)
	}
	if affected == 0 {
		var enabled int
		if err := d.db.QueryRowContext(ctx, "SELECT enabled FROM manual_port_mapping WHERE id = ?", mapping.ID).Scan(&enabled); err == sql.ErrNoRows {
			return ManualPortMapping{}, ErrManualPortMappingNotFound
		} else if err != nil {
			return ManualPortMapping{}, fmt.Errorf("check updated manual port mapping: %w", err)
		}
		return ManualPortMapping{}, ErrManualPortMappingInactive
	}
	return d.GetManualPortMapping(ctx, mapping.ID)
}

// DisableManualPortMapping performs the CRUD delete operation without losing
// administrator history or allowing LLDP changes to erase manual decisions.
func (d *Database) DisableManualPortMapping(ctx context.Context, id int64) error {
	result, err := d.db.ExecContext(ctx, `UPDATE manual_port_mapping
		SET enabled = 0, disabled_reason = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND enabled = 1`, ManualPortMappingDisabledByUser, id)
	if err != nil {
		return fmt.Errorf("disable manual port mapping: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read disabled manual port mapping count: %w", err)
	}
	if affected == 0 {
		var exists int
		if err := d.db.QueryRowContext(ctx, "SELECT 1 FROM manual_port_mapping WHERE id = ?", id).Scan(&exists); err == sql.ErrNoRows {
			return ErrManualPortMappingNotFound
		} else if err != nil {
			return fmt.Errorf("check disabled manual port mapping: %w", err)
		}
	}
	return nil
}

// SupersedeManualPortMappingByLLDP keeps the administrator record as history
// while releasing its active port/NIC uniqueness reservations. The reason
// records whether AUTO confirmed the same relation or won a physical conflict.
func (d *Database) SupersedeManualPortMappingByLLDP(ctx context.Context, id int64, reason string) error {
	if !validLLDPDisableReason(reason) {
		return fmt.Errorf("invalid LLDP manual mapping disable reason %q", reason)
	}
	result, err := d.db.ExecContext(ctx, `UPDATE manual_port_mapping
		SET enabled = 0, disabled_reason = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND enabled = 1`, reason, id)
	if err != nil {
		return fmt.Errorf("supersede manual port mapping by LLDP: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read superseded manual port mapping count: %w", err)
	}
	if affected == 0 {
		return ErrManualPortMappingNotFound
	}
	return nil
}

func validLLDPDisableReason(reason string) bool {
	switch reason {
	case ManualPortMappingDisabledByLLDPMatch,
		ManualPortMappingDisabledByLLDPPortConflict,
		ManualPortMappingDisabledByLLDPNICConflict,
		ManualPortMappingDisabledByLLDPConflict:
		return true
	default:
		return false
	}
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanManualPortMapping(row rowScanner) (ManualPortMapping, error) {
	var mapping ManualPortMapping
	var enabled int
	var switchPortNodeID sql.NullString
	var disabledReason sql.NullString
	err := row.Scan(
		&mapping.ID, &mapping.SwitchNodeID, &mapping.SwitchName,
		&switchPortNodeID, &mapping.SwitchPortName,
		&mapping.HostNodeID, &mapping.HostName,
		&mapping.HostNICNodeID, &mapping.HostNICName,
		&enabled, &disabledReason, &mapping.CreatedAt, &mapping.UpdatedAt,
	)
	mapping.SwitchPortNodeID = switchPortNodeID.String
	mapping.DisabledReason = disabledReason.String
	mapping.Enabled = enabled != 0
	return mapping, err
}

func nullableText(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func manualPortMappingWriteError(operation string, err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
		return fmt.Errorf("%w: %s", ErrManualPortMappingConflict, strings.ToLower(operation))
	}
	return fmt.Errorf("%s manual port mapping: %w", operation, err)
}
