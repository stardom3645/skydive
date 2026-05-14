package common

import (
	"database/sql"
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
	Endpoint string
	Username string
}

func IsMoldConsoleEnabled() bool {
	return config.GetBool("mold.console.enabled")
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

func GetMoldAPIConfig() MoldAPIConfig {
	return MoldAPIConfig{
		Endpoint: config.GetString("mold.api.endpoint"),
		Username: config.GetString("mold.api.username"),
	}
}

func OpenMoldDB() (*sql.DB, error) {
	dbCfg := GetMoldDBConfig()
	password, err := readPasswordFile(dbCfg.PasswordFile)
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

func GetMoldAdminKeys() (string, string, error) {
	apiCfg := GetMoldAPIConfig()
	db, err := OpenMoldDB()
	if err != nil {
		return "", "", err
	}
	defer db.Close()

	return getMoldAPIKeysFromDB(db, apiCfg.Username)
}

func getMoldAPIKeysFromDB(db *sql.DB, username string) (string, string, error) {
	var apiKey, secretKey string
	query := `
		SELECT k.api_key, k.secret_key
		FROM user u
		JOIN api_keypair k ON k.user_id = u.id
		WHERE u.username = ?
		  AND k.removed IS NULL
		  AND (k.start_date IS NULL OR k.start_date <= NOW())
		  AND (k.end_date IS NULL OR k.end_date >= NOW())
		ORDER BY k.created DESC, k.id DESC
		LIMIT 1`
	err := db.QueryRow(query, username).Scan(&apiKey, &secretKey)
	if err != nil {
		return "", "", err
	}

	return apiKey, secretKey, nil
}

func readPasswordFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("mold.db.passwordFile is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", fmt.Errorf("mold db password is empty")
	}

	return password, nil
}
