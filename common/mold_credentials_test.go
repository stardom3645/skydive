package common

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/skydive-project/skydive/config"
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

func (s *testMoldCredentialsStore) DeleteMoldAPICredentials(context.Context) error {
	s.apiKey, s.secretKey, s.configured = "", "", false
	return nil
}

func (s *testMoldCredentialsStore) DeleteMoldCredentials(context.Context) error {
	s.apiKey, s.secretKey, s.configured = "", "", false
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

func TestWriteMoldCredentialsStoresOnlyAPI(t *testing.T) {
	store := &testMoldCredentialsStore{}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })

	if err := WriteMoldCredentials("api-value", "secret-value"); err != nil {
		t.Fatal(err)
	}
	if store.apiKey != "api-value" || store.secretKey != "secret-value" || !store.configured {
		t.Fatalf("API credentials were not stored: %+v", store)
	}
}

func TestDeleteMoldCredentialsClearsConfiguredStatus(t *testing.T) {
	store := &testMoldCredentialsStore{apiKey: "api", secretKey: "secret", configured: true}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })
	if err := DeleteMoldCredentials(); err != nil {
		t.Fatal(err)
	}
	if store.configured {
		t.Fatal("API credentials still configured after delete")
	}
}

func TestDeleteMoldAPICredentialsDoesNotTouchCloudStackFiles(t *testing.T) {
	store := &testMoldCredentialsStore{apiKey: "api", secretKey: "secret", configured: true}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })
	propertiesPath, keyPath := writeTestMoldDBProperties(t, "db-password")
	setMoldDBTestPaths(t, propertiesPath, keyPath)
	if err := DeleteMoldAPICredentials(); err != nil {
		t.Fatal(err)
	}
	apiConfigured, dbConfigured, err := MoldCredentialsConfigured()
	if err != nil || apiConfigured || !dbConfigured {
		t.Fatalf("configured status after API delete = api:%v db:%v err:%v", apiConfigured, dbConfigured, err)
	}
}

func TestUnconfiguredMoldDBCacheRefreshesStayQuiet(t *testing.T) {
	store := &testMoldCredentialsStore{}
	SetMoldAPICredentialsStore(store)
	t.Cleanup(func() { SetMoldAPICredentialsStore(nil) })
	setMoldDBTestPaths(t, filepath.Join(t.TempDir(), "missing.properties"), filepath.Join(t.TempDir(), "missing.key"))

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

func setMoldDBTestPaths(t *testing.T, propertiesPath, keyPath string) {
	t.Helper()
	global := config.GetConfig()
	oldProperties := config.GetString("mold.db.propertiesFile")
	oldKey := config.GetString("mold.db.managementKeyFile")
	global.Set("mold.db.propertiesFile", propertiesPath)
	global.Set("mold.db.managementKeyFile", keyPath)
	t.Cleanup(func() {
		global.Set("mold.db.propertiesFile", oldProperties)
		global.Set("mold.db.managementKeyFile", oldKey)
	})
}

func writeTestMoldDBProperties(t *testing.T, password string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	propertiesPath := filepath.Join(dir, "db.properties")
	keyPath := filepath.Join(dir, "key")
	managementKey := "test-management-key"
	encoded := encryptCloudStackV2ForTest(t, password, managementKey)
	contents := "db.cloud.username=cloud\n" +
		"db.cloud.password=ENC(" + encoded + ")\n" +
		"db.cloud.host=localhost\n" +
		"db.cloud.port=3306\n" +
		"db.cloud.name=cloud\n" +
		"db.cloud.encryption.type=file\n" +
		"db.cloud.encryptor.version=V2\n"
	if err := os.WriteFile(propertiesPath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(managementKey+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	return propertiesPath, keyPath
}
