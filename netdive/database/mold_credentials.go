// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	credentialKeySize = 32
	credentialID      = 1
)

var credentialAdditionalData = []byte("netdive:mold-api-credentials:v1")

type moldCredentialsPayload struct {
	APIKey     string `json:"apiKey,omitempty"`
	SecretKey  string `json:"secretKey,omitempty"`
	DBPassword string `json:"dbPassword,omitempty"`
}

// LoadMoldAPICredentials decrypts the configured Mold credential pair. The
// boolean is false when no pair has been stored yet.
func (d *Database) LoadMoldAPICredentials(ctx context.Context) (string, string, bool, error) {
	payload, configured, err := d.loadMoldCredentialsPayload(ctx)
	if err != nil || !configured {
		return "", "", false, err
	}
	if strings.TrimSpace(payload.APIKey) == "" || strings.TrimSpace(payload.SecretKey) == "" {
		return "", "", false, nil
	}
	return payload.APIKey, payload.SecretKey, true, nil
}

// LoadMoldDBPassword decrypts the Mold database password. The boolean is false
// until the bootstrap password file has been imported.
func (d *Database) LoadMoldDBPassword(ctx context.Context) (string, bool, error) {
	payload, configured, err := d.loadMoldCredentialsPayload(ctx)
	if err != nil || !configured {
		return "", false, err
	}
	if strings.TrimSpace(payload.DBPassword) == "" {
		return "", false, nil
	}
	return payload.DBPassword, true, nil
}

func (d *Database) loadMoldCredentialsPayload(ctx context.Context) (moldCredentialsPayload, bool, error) {
	payload := moldCredentialsPayload{}
	if d == nil || d.db == nil {
		return payload, false, nil
	}

	var nonce, ciphertext []byte
	err := d.db.QueryRowContext(ctx,
		"SELECT nonce, ciphertext FROM mold_api_credentials WHERE id = ?", credentialID,
	).Scan(&nonce, &ciphertext)
	if err != nil {
		if err == sql.ErrNoRows {
			return payload, false, nil
		}
		return payload, false, fmt.Errorf("read encrypted Mold credentials: %w", err)
	}

	key, err := d.readCredentialKey(false)
	if err != nil {
		return payload, false, err
	}
	plaintext, err := openCredentialPayload(key, nonce, ciphertext)
	if err != nil {
		return payload, false, fmt.Errorf("decrypt Mold credentials: %w", err)
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return payload, false, fmt.Errorf("decode Mold credentials: %w", err)
	}
	return payload, true, nil
}

// SaveMoldAPICredentials encrypts and atomically upserts a Mold credential
// pair. Plaintext values are never written to SQLite.
func (d *Database) SaveMoldAPICredentials(ctx context.Context, apiKey, secretKey string) error {
	return d.SaveMoldCredentials(ctx, apiKey, secretKey, "")
}

// SaveMoldCredentials updates the API pair and, when non-empty, the database
// password in one encrypted payload write. An empty database password preserves
// the imported/default value.
func (d *Database) SaveMoldCredentials(ctx context.Context, apiKey, secretKey, dbPassword string) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("Netdive database is not available")
	}
	apiKey = strings.TrimSpace(apiKey)
	secretKey = strings.TrimSpace(secretKey)
	dbPassword = strings.TrimSpace(dbPassword)
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("Mold API credentials must not be empty")
	}

	d.credentialMu.Lock()
	defer d.credentialMu.Unlock()
	payload, configured, err := d.loadMoldCredentialsPayload(ctx)
	if err != nil {
		return err
	}
	if !configured {
		payload = moldCredentialsPayload{}
	}
	payload.APIKey = apiKey
	payload.SecretKey = secretKey
	if dbPassword != "" {
		payload.DBPassword = dbPassword
	}
	if strings.TrimSpace(payload.DBPassword) == "" {
		return fmt.Errorf("Mold database password must not be empty")
	}
	return d.saveMoldCredentialsPayload(ctx, payload)
}

// SaveMoldDBPassword encrypts the Mold database password while preserving any
// API credential pair already stored in the same payload.
func (d *Database) SaveMoldDBPassword(ctx context.Context, password string) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("Netdive database is not available")
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return fmt.Errorf("Mold database password must not be empty")
	}
	d.credentialMu.Lock()
	defer d.credentialMu.Unlock()
	payload, configured, err := d.loadMoldCredentialsPayload(ctx)
	if err != nil {
		return err
	}
	if !configured {
		payload = moldCredentialsPayload{}
	}
	payload.DBPassword = password
	return d.saveMoldCredentialsPayload(ctx, payload)
}

func (d *Database) saveMoldCredentialsPayload(ctx context.Context, credentials moldCredentialsPayload) error {
	payload, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode Mold credentials: %w", err)
	}
	key, err := d.readCredentialKey(true)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := sealCredentialPayload(key, payload)
	if err != nil {
		return fmt.Errorf("encrypt Mold credentials: %w", err)
	}

	_, err = d.db.ExecContext(ctx, `INSERT INTO mold_api_credentials (id, nonce, ciphertext, updated_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET nonce = excluded.nonce, ciphertext = excluded.ciphertext,
		updated_at = excluded.updated_at`, credentialID, nonce, ciphertext)
	if err != nil {
		return fmt.Errorf("store encrypted Mold credentials: %w", err)
	}
	return nil
}

func sealCredentialPayload(key, plaintext []byte) ([]byte, []byte, error) {
	gcm, err := newCredentialGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, credentialAdditionalData), nil
}

func openCredentialPayload(key, nonce, ciphertext []byte) ([]byte, error) {
	gcm, err := newCredentialGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("invalid credential nonce length")
	}
	return gcm.Open(nil, nonce, ciphertext, credentialAdditionalData)
}

func newCredentialGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (d *Database) readCredentialKey(create bool) ([]byte, error) {
	path := strings.TrimSpace(d.credentialKeyFile)
	if path == "" {
		return nil, fmt.Errorf("custom.database.credentialKeyFile must not be empty")
	}
	encoded, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("inspect credential encryption key: %w", statErr)
		}
		if info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("credential encryption key %q must have 0600 permissions", path)
		}
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
		if decodeErr != nil || len(key) != credentialKeySize {
			return nil, fmt.Errorf("credential encryption key %q is invalid", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) || !create {
		return nil, fmt.Errorf("read credential encryption key %q: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("prepare credential key directory: %w", err)
	}
	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate credential encryption key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return d.readCredentialKey(false)
	}
	if err != nil {
		return nil, fmt.Errorf("create credential encryption key %q: %w", path, err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("write credential encryption key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync credential encryption key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close credential encryption key: %w", err)
	}
	remove = false
	return key, nil
}
