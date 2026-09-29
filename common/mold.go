package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
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
	LoadMoldDBPassword(context.Context) (string, bool, error)
	SaveMoldDBPassword(context.Context, string) error
	SaveMoldCredentials(context.Context, string, string, string) error
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

// DeleteMoldCredentials removes only the encrypted Mold credential payload.
// Netdive's database, encryption key, event history, and manual mappings stay intact.
func DeleteMoldCredentials() error {
	moldAPIKeysMu.RLock()
	store := moldAPICredentialsStore
	moldAPIKeysMu.RUnlock()
	if store == nil {
		return fmt.Errorf("encrypted Netdive credential store is not available")
	}
	return store.DeleteMoldCredentials(context.Background())
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

func GetMoldDBConfig() MoldDBConfig {
	return MoldDBConfig{
		Host: config.GetString("mold.db.host"),
		Port: config.GetInt("mold.db.port"),
		Name: config.GetString("mold.db.name"),
		User: config.GetString("mold.db.user"),
	}
}

func OpenMoldDB() (*sql.DB, error) {
	password, err := ReadMoldDBPassword()
	if err != nil {
		return nil, err
	}
	return openMoldDBWithPassword(password)
}

func openMoldDBWithPassword(password string) (*sql.DB, error) {
	dbCfg := GetMoldDBConfig()
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

// TestMoldDBPassword verifies the configured Mold database endpoint without
// persisting the supplied plaintext password.
func TestMoldDBPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return ErrMoldDBPasswordNotConfigured
	}
	db, err := openMoldDBWithPassword(password)
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
