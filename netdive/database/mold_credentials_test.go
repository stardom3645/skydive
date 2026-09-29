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
	writeTestManagementKey(t, cfg.ManagementKeyFile)
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

	keyInfo, err := os.Stat(cfg.ManagementKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0640 {
		t.Fatalf("management key permissions = %o, want 640", keyInfo.Mode().Perm())
	}

	if err := db.SaveMoldAPICredentials(context.Background(), "new-api", "new-secret"); err != nil {
		t.Fatal(err)
	}
}

func TestMoldAPICredentialsMissingAndTampered(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(filepath.Join(dir, "netdive.db"))
	writeTestManagementKey(t, cfg.ManagementKeyFile)
	db, err := Open(context.Background(), cfg)
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

func TestDeleteMoldCredentialsRemovesOnlyCredentialRow(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(filepath.Join(dir, "netdive.db"))
	writeTestManagementKey(t, cfg.ManagementKeyFile)
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SaveMoldAPICredentials(ctx, "api", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMoldCredentials(ctx); err != nil {
		t.Fatal(err)
	}
	_, _, apiConfigured, err := db.LoadMoldAPICredentials(ctx)
	if err != nil || apiConfigured {
		t.Fatalf("API configured after delete = %v, err=%v", apiConfigured, err)
	}
	if _, err := os.Stat(db.managementKeyFile); err != nil {
		t.Fatalf("CloudStack management key should be untouched: %v", err)
	}
}

func TestDeleteMoldAPICredentialsRemovesAPIOnly(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(filepath.Join(dir, "netdive.db"))
	writeTestManagementKey(t, cfg.ManagementKeyFile)
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SaveMoldAPICredentials(ctx, "api", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMoldAPICredentials(ctx); err != nil {
		t.Fatal(err)
	}
	_, _, apiConfigured, err := db.LoadMoldAPICredentials(ctx)
	if err != nil || apiConfigured {
		t.Fatalf("API configured after API delete = %v, err=%v", apiConfigured, err)
	}
}

func writeTestManagementKey(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("cloudstack-management-key\n"), 0640); err != nil {
		t.Fatal(err)
	}
}
