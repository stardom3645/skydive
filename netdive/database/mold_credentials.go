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
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const credentialID = 1

var credentialAdditionalData = []byte("netdive:mold-api-credentials:v1")

type moldCredentialsPayload struct {
	APIKey    string `json:"apiKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
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

	key, err := d.readCredentialKey()
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
	if d == nil || d.db == nil {
		return fmt.Errorf("Netdive database is not available")
	}
	apiKey = strings.TrimSpace(apiKey)
	secretKey = strings.TrimSpace(secretKey)
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("Mold API credentials must not be empty")
	}

	d.credentialMu.Lock()
	defer d.credentialMu.Unlock()
	return d.saveMoldCredentialsPayload(ctx, moldCredentialsPayload{APIKey: apiKey, SecretKey: secretKey})
}

// DeleteMoldAPICredentials removes the operator-managed API credential pair.
func (d *Database) DeleteMoldAPICredentials(ctx context.Context) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("Netdive database is not available")
	}
	d.credentialMu.Lock()
	defer d.credentialMu.Unlock()
	_, err := d.db.ExecContext(ctx, "DELETE FROM mold_api_credentials WHERE id = ?", credentialID)
	return err
}

// DeleteMoldCredentials removes the singleton encrypted credential payload.
// The separate encryption key is retained for the next setup.
func (d *Database) DeleteMoldCredentials(ctx context.Context) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("Netdive database is not available")
	}
	d.credentialMu.Lock()
	defer d.credentialMu.Unlock()
	_, err := d.db.ExecContext(ctx, "DELETE FROM mold_api_credentials WHERE id = ?", credentialID)
	if err != nil {
		return fmt.Errorf("delete encrypted Mold credentials: %w", err)
	}
	return nil
}

func (d *Database) saveMoldCredentialsPayload(ctx context.Context, credentials moldCredentialsPayload) error {
	payload, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode Mold credentials: %w", err)
	}
	key, err := d.readCredentialKey()
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

func (d *Database) readCredentialKey() ([]byte, error) {
	path := strings.TrimSpace(d.managementKeyFile)
	if path == "" {
		return nil, fmt.Errorf("mold.db.managementKeyFile must not be empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credential encryption key %q: %w", path, err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, fmt.Errorf("credential encryption key %q is empty", path)
	}
	// CloudStack V2 derives its AES-256 key by hashing the management key.
	// Reusing that existing key avoids creating another secret on the template.
	key := sha256.Sum256([]byte(strings.TrimSpace(string(raw))))
	return key[:], nil
}
