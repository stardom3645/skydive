package common

import (
	"bytes"
	"context"
	"log"
	"testing"
)

type testMoldCredentialsStore struct {
	apiKey, secretKey, dbPassword string
	configured                    bool
}

func (s *testMoldCredentialsStore) LoadMoldAPICredentials(context.Context) (string, string, bool, error) {
	return s.apiKey, s.secretKey, s.configured, nil
}

func (s *testMoldCredentialsStore) SaveMoldAPICredentials(_ context.Context, apiKey, secretKey string) error {
	s.apiKey, s.secretKey, s.configured = apiKey, secretKey, true
	return nil
}

func (s *testMoldCredentialsStore) LoadMoldDBPassword(context.Context) (string, bool, error) {
	return s.dbPassword, s.dbPassword != "", nil
}

func (s *testMoldCredentialsStore) SaveMoldDBPassword(_ context.Context, password string) error {
	s.dbPassword = password
	return nil
}

func (s *testMoldCredentialsStore) SaveMoldCredentials(_ context.Context, apiKey, secretKey, dbPassword string) error {
	s.apiKey, s.secretKey, s.configured = apiKey, secretKey, true
	if dbPassword != "" {
		s.dbPassword = dbPassword
	}
	return nil
}

func (s *testMoldCredentialsStore) DeleteMoldCredentials(context.Context) error {
	s.apiKey, s.secretKey, s.dbPassword, s.configured = "", "", "", false
	return nil
}

func TestMoldAPIKeysUseConfiguredStore(t *testing.T) {
	store := &testMoldCredentialsStore{apiKey: "api-value", secretKey: "secret-value", configured: true}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })

	apiKey, secretKey, err := ReadMoldAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if apiKey != "api-value" || secretKey != "secret-value" {
		t.Fatalf("stored credentials = %q/%q", apiKey, secretKey)
	}
}

func TestMoldAPIKeysRejectMissingEncryptedStore(t *testing.T) {
	SetMoldAPICredentialsStore(nil)
	if _, _, err := ReadMoldAPIKeys(); err == nil {
		t.Fatal("expected missing encrypted credential store to fail")
	}
}

func TestWriteMoldCredentialsStoresDBPassword(t *testing.T) {
	store := &testMoldCredentialsStore{}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })

	if err := WriteMoldCredentials("api-value", "secret-value", "db-password"); err != nil {
		t.Fatal(err)
	}
	apiConfigured, dbConfigured, err := MoldCredentialsConfigured()
	if err != nil || !apiConfigured || !dbConfigured {
		t.Fatalf("configured status = api:%v db:%v err:%v", apiConfigured, dbConfigured, err)
	}
	password, err := ReadMoldDBPassword()
	if err != nil || password != "db-password" {
		t.Fatalf("DB password = %q, err=%v", password, err)
	}
}

func TestDeleteMoldCredentialsClearsConfiguredStatus(t *testing.T) {
	store := &testMoldCredentialsStore{apiKey: "api", secretKey: "secret", dbPassword: "password", configured: true}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })
	if err := DeleteMoldCredentials(); err != nil {
		t.Fatal(err)
	}
	apiConfigured, dbConfigured, err := MoldCredentialsConfigured()
	if err != nil || apiConfigured || dbConfigured {
		t.Fatalf("configured status after delete = api:%v db:%v err:%v", apiConfigured, dbConfigured, err)
	}
}

func TestUnconfiguredMoldDBCacheRefreshesStayQuiet(t *testing.T) {
	store := &testMoldCredentialsStore{}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })

	var output bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	LoadVmNameMapFromCloudstack()
	LoadVMNetworkMapFromCloudstack()
	LoadVMDetailMapFromCloudstack()

	if output.Len() != 0 {
		t.Fatalf("unconfigured Mold DB produced repeated cache logs: %s", output.String())
	}
}
