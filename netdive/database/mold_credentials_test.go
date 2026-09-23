// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMoldAPICredentialsEncryptedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(filepath.Join(dir, "netdive.db"))
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	apiKey := "api-key-must-not-appear-in-sqlite"
	secretKey := "secret-key-must-not-appear-in-sqlite"
	if err := db.SaveMoldAPICredentials(context.Background(), apiKey, secretKey); err != nil {
		t.Fatal(err)
	}
	gotAPI, gotSecret, configured, err := db.LoadMoldAPICredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !configured || gotAPI != apiKey || gotSecret != secretKey {
		t.Fatalf("unexpected credential round trip: configured=%v api=%q secret=%q", configured, gotAPI, gotSecret)
	}

	var nonce, ciphertext []byte
	if err := db.SQLDB().QueryRow("SELECT nonce, ciphertext FROM mold_api_credentials WHERE id = 1").Scan(&nonce, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(apiKey)) || bytes.Contains(ciphertext, []byte(secretKey)) {
		t.Fatal("SQLite ciphertext contains a plaintext credential")
	}
	if len(nonce) != 12 {
		t.Fatalf("nonce length = %d, want 12", len(nonce))
	}

	keyInfo, err := os.Stat(cfg.CredentialKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0600 {
		t.Fatalf("credential key permissions = %o, want 600", keyInfo.Mode().Perm())
	}
}

func TestMoldAPICredentialsMissingAndTampered(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), testConfig(filepath.Join(dir, "netdive.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, _, configured, err := db.LoadMoldAPICredentials(context.Background())
	if err != nil || configured {
		t.Fatalf("empty credentials: configured=%v err=%v", configured, err)
	}
	if err := db.SaveMoldAPICredentials(context.Background(), "api", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLDB().Exec("UPDATE mold_api_credentials SET ciphertext = randomblob(length(ciphertext)) WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.LoadMoldAPICredentials(context.Background()); err == nil {
		t.Fatal("expected tampered ciphertext to be rejected")
	}
}
