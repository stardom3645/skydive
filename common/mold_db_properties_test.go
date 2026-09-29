package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestLoadMoldDBSettingsDecryptsCloudStackV2(t *testing.T) {
	propertiesPath, keyPath := writeTestMoldDBProperties(t, "db-secret-value")
	settings, err := loadMoldDBSettings(propertiesPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Password != "db-secret-value" {
		t.Fatalf("password = %q", settings.Password)
	}
	if settings.Config.Host != "localhost" || settings.Config.Port != 3306 || settings.Config.User != "cloud" || settings.Config.Name != "cloud" {
		t.Fatalf("unexpected DB config: %+v", settings.Config)
	}
}

func TestLoadMoldDBSettingsRejectsWrongKey(t *testing.T) {
	propertiesPath, _ := writeTestMoldDBProperties(t, "db-secret-value")
	_, wrongKeyPath := writeTestMoldDBProperties(t, "other")
	if err := os.WriteFile(wrongKeyPath, []byte("wrong-management-key\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMoldDBSettings(propertiesPath, wrongKeyPath); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("wrong key error = %v", err)
	}
}

func TestLoadMoldDBSettingsSupportsPlainPassword(t *testing.T) {
	propertiesPath, keyPath := writeTestMoldDBProperties(t, "unused")
	properties := "db.cloud.username=cloud\n" +
		"db.cloud.password=plain-password\n" +
		"db.cloud.host=ccvm\n" +
		"db.cloud.port=3307\n" +
		"db.cloud.name=cloud\n"
	if err := os.WriteFile(propertiesPath, []byte(properties), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := loadMoldDBSettings(propertiesPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Password != "plain-password" || settings.Config.Port != 3307 {
		t.Fatalf("unexpected settings: %+v", settings)
	}
}

func encryptCloudStackV2ForTest(t *testing.T, plaintext, managementKey string) string {
	t.Helper()
	key := sha256.Sum256([]byte(managementKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	output := append(append([]byte{}, nonce...), gcm.Seal(nil, nonce, []byte(plaintext), nil)...)
	return base64.StdEncoding.EncodeToString(output)
}
