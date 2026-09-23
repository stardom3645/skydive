package common

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type testMoldCredentialsStore struct {
	apiKey, secretKey string
	configured        bool
}

func (s *testMoldCredentialsStore) LoadMoldAPICredentials(context.Context) (string, string, bool, error) {
	return s.apiKey, s.secretKey, s.configured, nil
}

func (s *testMoldCredentialsStore) SaveMoldAPICredentials(_ context.Context, apiKey, secretKey string) error {
	s.apiKey, s.secretKey, s.configured = apiKey, secretKey, true
	return nil
}

func TestWriteMoldAPIKeys(t *testing.T) {
	dir := t.TempDir()
	apiPath := filepath.Join(dir, "mold-api-key")
	secretPath := filepath.Join(dir, "mold-secret-key")

	if err := writeMoldAPIKeys(apiPath, secretPath, " api-value ", " secret-value "); err != nil {
		t.Fatalf("writeMoldAPIKeys failed: %v", err)
	}

	for path, expected := range map[string]string{
		apiPath:    "api-value\n",
		secretPath: "secret-value\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		if string(data) != expected {
			t.Fatalf("unexpected content for %s: %q", path, data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("unexpected permissions for %s: %o", path, info.Mode().Perm())
		}
	}
}

func TestWriteMoldAPIKeysRejectsEmptyValues(t *testing.T) {
	dir := t.TempDir()
	err := writeMoldAPIKeys(filepath.Join(dir, "api"), filepath.Join(dir, "secret"), "", "secret")
	if err == nil {
		t.Fatal("expected empty API key to be rejected")
	}
}

func TestMoldAPIKeysUseConfiguredStore(t *testing.T) {
	store := &testMoldCredentialsStore{}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })

	if err := WriteMoldAPIKeys(" api-value ", " secret-value "); err != nil {
		t.Fatal(err)
	}
	apiKey, secretKey, err := ReadMoldAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if apiKey != "api-value" || secretKey != "secret-value" {
		t.Fatalf("stored credentials = %q/%q", apiKey, secretKey)
	}
}
