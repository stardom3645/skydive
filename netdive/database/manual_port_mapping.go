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
)

// ManualPortMapping is a persisted administrator-supplied physical relation.
// Node IDs are authoritative; names are snapshots used only for display.
type ManualPortMapping struct {
	ID               int64  `json:"id"`
	SwitchNodeID     string `json:"switchNodeId"`
	SwitchName       string `json:"switchName,omitempty"`
	SwitchPortNodeID string `json:"switchPortNodeId"`
	SwitchPortName   string `json:"switchPortName,omitempty"`
	HostNodeID       string `json:"hostNodeId"`
	HostName         string `json:"hostName,omitempty"`
	HostNICNodeID    string `json:"hostNicNodeId"`
	HostNICName      string `json:"hostNicName,omitempty"`
	Enabled          bool   `json:"enabled"`
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
		host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, created_at, updated_at
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
		host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, created_at, updated_at
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
		mapping.SwitchNodeID, mapping.SwitchName, mapping.SwitchPortNodeID, mapping.SwitchPortName,
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

// UpdateManualPortMapping replaces the stable endpoints and enabled state of
// one mapping while preserving its creation timestamp.
func (d *Database) UpdateManualPortMapping(ctx context.Context, mapping ManualPortMapping) (ManualPortMapping, error) {
	result, err := d.db.ExecContext(ctx, `UPDATE manual_port_mapping SET
		switch_node_id = ?, switch_name = ?, switch_port_node_id = ?, switch_port_name = ?,
		host_node_id = ?, host_name = ?, host_nic_node_id = ?, host_nic_name = ?, enabled = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`,
		mapping.SwitchNodeID, mapping.SwitchName, mapping.SwitchPortNodeID, mapping.SwitchPortName,
		mapping.HostNodeID, mapping.HostName, mapping.HostNICNodeID, mapping.HostNICName,
		boolInt(mapping.Enabled), mapping.ID)
	if err != nil {
		return ManualPortMapping{}, manualPortMappingWriteError("update", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ManualPortMapping{}, fmt.Errorf("read updated manual port mapping count: %w", err)
	}
	if affected == 0 {
		return ManualPortMapping{}, ErrManualPortMappingNotFound
	}
	return d.GetManualPortMapping(ctx, mapping.ID)
}

// DisableManualPortMapping performs the CRUD delete operation without losing
// administrator history or allowing LLDP changes to erase manual decisions.
func (d *Database) DisableManualPortMapping(ctx context.Context, id int64) error {
	result, err := d.db.ExecContext(ctx, `UPDATE manual_port_mapping
		SET enabled = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("disable manual port mapping: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read disabled manual port mapping count: %w", err)
	}
	if affected == 0 {
		return ErrManualPortMappingNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanManualPortMapping(row rowScanner) (ManualPortMapping, error) {
	var mapping ManualPortMapping
	var enabled int
	err := row.Scan(
		&mapping.ID, &mapping.SwitchNodeID, &mapping.SwitchName,
		&mapping.SwitchPortNodeID, &mapping.SwitchPortName,
		&mapping.HostNodeID, &mapping.HostName,
		&mapping.HostNICNodeID, &mapping.HostNICName,
		&enabled, &mapping.CreatedAt, &mapping.UpdatedAt,
	)
	mapping.Enabled = enabled != 0
	return mapping, err
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
