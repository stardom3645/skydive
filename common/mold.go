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

func IsMoldConsoleEnabled() bool {
	return config.GetBool("mold.console.enabled")
}

func GetMoldConsoleAPIEndpoint() string {
	return config.GetString("mold.console.apiEndpoint")
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
