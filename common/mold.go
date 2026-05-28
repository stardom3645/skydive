package common

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/skydive-project/skydive/config"
)

type MoldDBConfig struct {
	Host         string
	Port         int
	Name         string
	User         string
	PasswordFile string
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

func GetMoldDBConfig() MoldDBConfig {
	return MoldDBConfig{
		Host:         config.GetString("mold.db.host"),
		Port:         config.GetInt("mold.db.port"),
		Name:         config.GetString("mold.db.name"),
		User:         config.GetString("mold.db.user"),
		PasswordFile: config.GetString("mold.db.passwordFile"),
	}
}

func OpenMoldDB() (*sql.DB, error) {
	dbCfg := GetMoldDBConfig()
	password, err := readSecretFile(dbCfg.PasswordFile, "mold.db.passwordFile")
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
