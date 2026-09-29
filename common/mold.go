package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	mysql "github.com/go-sql-driver/mysql"

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
// credential repository. Credentials are never read from plaintext files.
type MoldCredentialsStore interface {
	LoadMoldAPICredentials(context.Context) (string, string, bool, error)
	SaveMoldAPICredentials(context.Context, string, string) error
	DeleteMoldAPICredentials(context.Context) error
	DeleteMoldCredentials(context.Context) error
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
	Endpoint string
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
		Endpoint: config.GetString("mold.api.endpoint"),
	}
}

func ReadMoldAPIKeys() (string, string, error) {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()

	if store == nil {
		return "", "", fmt.Errorf("encrypted Netdive credential store is not available")
	}
	apiKey, secretKey, configured, err := store.LoadMoldAPICredentials(context.Background())
	if err != nil {
		return "", "", err
	}
	if configured {
		return apiKey, secretKey, nil
	}
	return "", "", ErrMoldAPICredentialsNotConfigured
}

// MoldCredentialsConfigured reports which encrypted values exist without
// exposing them.
func MoldCredentialsConfigured() (bool, bool, error) {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return false, false, fmt.Errorf("encrypted Netdive credential store is not available")
	}
	_, _, apiConfigured, err := store.LoadMoldAPICredentials(context.Background())
	if err != nil {
		return false, false, err
	}
	_, err = loadMoldDBSettingsFromConfig()
	dbConfigured := err == nil
	return apiConfigured, dbConfigured, nil
}

// WriteMoldCredentials stores the API pair in encrypted SQLite. Mold's DB
// password remains owned by CloudStack and is never copied into Netdive's DB.
func WriteMoldCredentials(apiKey, secretKey string) error {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return fmt.Errorf("encrypted Netdive credential store is not available")
	}
	return store.SaveMoldAPICredentials(context.Background(), apiKey, secretKey)
}

// DeleteMoldAPICredentials clears only the operator-managed API pair.
func DeleteMoldAPICredentials() error {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return fmt.Errorf("encrypted Netdive credential store is not available")
	}
	return store.DeleteMoldAPICredentials(context.Background())
}

// DeleteMoldCredentials removes only the encrypted Mold API credential payload.
// Netdive's database, CloudStack configuration, event history, and mappings stay intact.
func DeleteMoldCredentials() error {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return fmt.Errorf("encrypted Netdive credential store is not available")
	}
	return store.DeleteMoldCredentials(context.Background())
}

// ReadMoldDBPassword reads and decrypts CloudStack's own db.properties value.
// The plaintext is used in memory only and is never copied to Netdive SQLite.
func ReadMoldDBPassword() (string, error) {
	settings, err := loadMoldDBSettingsFromConfig()
	if err != nil {
		return "", err
	}
	return settings.Password, nil
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
	settings, err := loadMoldDBSettingsFromConfig()
	if err != nil {
		return nil, err
	}
	return openMoldDB(settings.Config, settings.Password)
}

func openMoldDB(dbCfg MoldDBConfig, password string) (*sql.DB, error) {
	dsn := (&mysql.Config{
		User: dbCfg.User, Passwd: password, Net: "tcp",
		Addr: net.JoinHostPort(dbCfg.Host, strconv.Itoa(dbCfg.Port)), DBName: dbCfg.Name,
	}).FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// TestMoldDBConnection verifies CloudStack's configured database endpoint.
func TestMoldDBConnection() error {
	db, err := OpenMoldDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var one int
	return db.QueryRow("SELECT 1").Scan(&one)
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
