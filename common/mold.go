package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	"github.com/skydive-project/skydive/config"
)

var (
	moldAPIKeysMu           sync.RWMutex
	moldAPICredentialsStore MoldCredentialsStore
)

var (
	ErrMoldAPICredentialsNotConfigured = errors.New("Mold API credentials are not configured")
	ErrMoldDBPasswordNotConfigured     = errors.New("Mold database password is not configured")
)

// MoldCredentialsStore is implemented by Netdive's encrypted local SQLite
// credential repository. A nil store preserves the legacy secret-file mode.
type MoldCredentialsStore interface {
	LoadMoldAPICredentials(context.Context) (string, string, bool, error)
	SaveMoldAPICredentials(context.Context, string, string) error
	LoadMoldDBPassword(context.Context) (string, bool, error)
	SaveMoldDBPassword(context.Context, string) error
	SaveMoldCredentials(context.Context, string, string, string) error
}

// SetMoldAPICredentialsStore selects encrypted SQLite storage for Mold API
// credentials. It is configured by the analyzer after opening its local DB.
func SetMoldAPICredentialsStore(store MoldCredentialsStore) {
	moldAPIKeysMu.Lock()
	defer moldAPIKeysMu.Unlock()
	moldAPICredentialsStore = store
}

type MoldDBConfig struct {
	Host string
	Port int
	Name string
	User string
}

type MoldAPIConfig struct {
	Endpoint      string
	APIKeyFile    string
	SecretKeyFile string
}

type SecretFileErrorReason string

const (
	SecretFileMissing SecretFileErrorReason = "missing"
	SecretFileRead    SecretFileErrorReason = "read"
	SecretFileEmpty   SecretFileErrorReason = "empty"
)

type SecretFileError struct {
	KeyName string
	Path    string
	Reason  SecretFileErrorReason
	Err     error
}

func (e *SecretFileError) Error() string {
	switch e.Reason {
	case SecretFileMissing:
		return fmt.Sprintf("%s is empty", e.KeyName)
	case SecretFileEmpty:
		return fmt.Sprintf("secret file %s is empty", e.KeyName)
	default:
		return fmt.Sprintf("failed to read secret file %s", e.KeyName)
	}
}

func (e *SecretFileError) Unwrap() error {
	return e.Err
}

func IsMoldConsoleEnabled() bool {
	return config.GetBool("mold.console.enabled")
}

func IsMoldConsoleMockAllowed() bool {
	return config.GetBool("mold.console.allowMock")
}

func GetMoldConsoleAPIEndpoint() string {
	return config.GetString("mold.console.apiEndpoint")
}

func GetMoldAPIConfig() MoldAPIConfig {
	return MoldAPIConfig{
		Endpoint:      config.GetString("mold.api.endpoint"),
		APIKeyFile:    config.GetString("mold.api.apiKeyFile"),
		SecretKeyFile: config.GetString("mold.api.secretKeyFile"),
	}
}

func ReadMoldAPIKeys() (string, string, error) {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()

	if store != nil {
		apiKey, secretKey, configured, err := store.LoadMoldAPICredentials(context.Background())
		if err != nil {
			return "", "", err
		}
		if configured {
			return apiKey, secretKey, nil
		}
		return "", "", ErrMoldAPICredentialsNotConfigured
	}

	apiCfg := GetMoldAPIConfig()
	apiKey, err := readSecretFile(apiCfg.APIKeyFile, "mold.api.apiKeyFile")
	if err != nil {
		return "", "", err
	}
	secretKey, err := readSecretFile(apiCfg.SecretKeyFile, "mold.api.secretKeyFile")
	if err != nil {
		return "", "", err
	}
	return apiKey, secretKey, nil
}

// WriteMoldAPIKeys replaces the configured Mold credentials without exposing
// them through the application configuration or requiring an analyzer restart.
func WriteMoldAPIKeys(apiKey, secretKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	secretKey = strings.TrimSpace(secretKey)
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("Mold API credentials must not be empty")
	}

	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store != nil {
		return store.SaveMoldAPICredentials(context.Background(), apiKey, secretKey)
	}

	apiCfg := GetMoldAPIConfig()
	return writeMoldAPIKeys(apiCfg.APIKeyFile, apiCfg.SecretKeyFile, apiKey, secretKey)
}

// MoldCredentialsConfigured reports which encrypted values exist without
// exposing them.
func MoldCredentialsConfigured() (bool, bool, error) {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		_, _, apiErr := ReadMoldAPIKeys()
		_, dbErr := ReadMoldDBPassword()
		return apiErr == nil, dbErr == nil, nil
	}
	_, _, apiConfigured, err := store.LoadMoldAPICredentials(context.Background())
	if err != nil {
		return false, false, err
	}
	_, dbConfigured, err := store.LoadMoldDBPassword(context.Background())
	if err != nil {
		return false, false, err
	}
	return apiConfigured, dbConfigured, nil
}

// WriteMoldCredentials stores all values in one encrypted SQLite payload. An
// empty database password preserves the previously stored value.
func WriteMoldCredentials(apiKey, secretKey, dbPassword string) error {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return fmt.Errorf("encrypted Netdive credential store is not available")
	}
	return store.SaveMoldCredentials(context.Background(), apiKey, secretKey, dbPassword)
}

// ReadMoldDBPassword reads only from Netdive's encrypted SQLite store.
func ReadMoldDBPassword() (string, error) {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store != nil {
		password, configured, err := store.LoadMoldDBPassword(context.Background())
		if err != nil {
			return "", err
		}
		if configured {
			return password, nil
		}
		return "", ErrMoldDBPasswordNotConfigured
	}
	return "", fmt.Errorf("encrypted Netdive credential store is not available")
}

func writeMoldAPIKeys(apiKeyPath, secretKeyPath, apiKey, secretKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	secretKey = strings.TrimSpace(secretKey)
	if apiKeyPath == "" || secretKeyPath == "" {
		return fmt.Errorf("Mold API credential file path is empty")
	}
	if apiKeyPath == secretKeyPath {
		return fmt.Errorf("Mold API credential file paths must be different")
	}
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("Mold API credentials must not be empty")
	}

	apiTemp, err := stageSecretFile(apiKeyPath, apiKey)
	if err != nil {
		return err
	}
	defer os.Remove(apiTemp)
	secretTemp, err := stageSecretFile(secretKeyPath, secretKey)
	if err != nil {
		return err
	}
	defer os.Remove(secretTemp)

	moldAPIKeysMu.Lock()
	defer moldAPIKeysMu.Unlock()
	if err := os.Rename(apiTemp, apiKeyPath); err != nil {
		return fmt.Errorf("failed to replace Mold API key: %w", err)
	}
	if err := os.Rename(secretTemp, secretKeyPath); err != nil {
		return fmt.Errorf("failed to replace Mold secret key: %w", err)
	}
	return nil
}

func stageSecretFile(path, value string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to prepare secret directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary secret file: %w", err)
	}
	tempPath := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return "", fmt.Errorf("failed to protect temporary secret file: %w", err)
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		return "", fmt.Errorf("failed to write temporary secret file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync temporary secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary secret file: %w", err)
	}
	remove = false
	return tempPath, nil
}

func GetMoldDBConfig() MoldDBConfig {
	return MoldDBConfig{
		Host: config.GetString("mold.db.host"),
		Port: config.GetInt("mold.db.port"),
		Name: config.GetString("mold.db.name"),
		User: config.GetString("mold.db.user"),
	}
}

func OpenMoldDB() (*sql.DB, error) {
	dbCfg := GetMoldDBConfig()
	password, err := ReadMoldDBPassword()
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", dbCfg.User, password, dbCfg.Host, dbCfg.Port, dbCfg.Name)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func ResolveVMIDFromNodeID(nodeID string) (string, error) {
	db, err := OpenMoldDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var vmID string
	query := `
		SELECT uuid
		FROM vm_instance
		WHERE removed IS NULL
		  AND (uuid = ? OR instance_name = ? OR name = ?)
		ORDER BY id DESC
		LIMIT 1`
	err = db.QueryRow(query, nodeID, nodeID, nodeID).Scan(&vmID)
	if err != nil {
		return "", err
	}

	return vmID, nil
}

func ResolveVMIDFromInstanceName(instanceName string) (string, error) {
	db, err := OpenMoldDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var vmID string
	query := `
		SELECT uuid
		FROM vm_instance
		WHERE removed IS NULL
		  AND instance_name = ?
		ORDER BY id DESC
		LIMIT 1`
	err = db.QueryRow(query, instanceName).Scan(&vmID)
	if err != nil {
		return "", err
	}

	return vmID, nil
}

func readSecretFile(path, keyName string) (string, error) {
	if path == "" {
		return "", &SecretFileError{
			KeyName: keyName,
			Path:    path,
			Reason:  SecretFileMissing,
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &SecretFileError{
				KeyName: keyName,
				Path:    path,
				Reason:  SecretFileMissing,
				Err:     err,
			}
		}
		return "", &SecretFileError{
			KeyName: keyName,
			Path:    path,
			Reason:  SecretFileRead,
			Err:     err,
		}
	}

	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", &SecretFileError{
			KeyName: keyName,
			Path:    path,
			Reason:  SecretFileEmpty,
		}
	}

	return password, nil
}
