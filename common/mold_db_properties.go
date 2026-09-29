// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package common

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/skydive-project/skydive/config"
)

const cloudStackV2NonceSize = 12

type moldDBSettings struct {
	Config   MoldDBConfig
	Password string
}

func loadMoldDBSettingsFromConfig() (moldDBSettings, error) {
	return loadMoldDBSettings(
		config.GetString("mold.db.propertiesFile"),
		config.GetString("mold.db.managementKeyFile"),
	)
}

func loadMoldDBSettings(propertiesPath, keyPath string) (moldDBSettings, error) {
	settings := moldDBSettings{}
	propertiesPath = strings.TrimSpace(propertiesPath)
	keyPath = strings.TrimSpace(keyPath)
	if propertiesPath == "" || keyPath == "" {
		return settings, fmt.Errorf("Mold database properties paths are not configured")
	}

	properties, err := readJavaProperties(propertiesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, fmt.Errorf("%w: %v", ErrMoldDBPasswordNotConfigured, err)
		}
		return settings, fmt.Errorf("read Mold database properties: %w", err)
	}
	passwordValue := strings.TrimSpace(properties["db.cloud.password"])
	if passwordValue == "" {
		return settings, ErrMoldDBPasswordNotConfigured
	}
	password := passwordValue
	if strings.HasPrefix(passwordValue, "ENC(") && strings.HasSuffix(passwordValue, ")") {
		if !strings.EqualFold(strings.TrimSpace(properties["db.cloud.encryption.type"]), "file") {
			return settings, fmt.Errorf("unsupported Mold database encryption type %q", properties["db.cloud.encryption.type"])
		}
		if !strings.EqualFold(strings.TrimSpace(properties["db.cloud.encryptor.version"]), "V2") {
			return settings, fmt.Errorf("unsupported Mold database encryptor version %q", properties["db.cloud.encryptor.version"])
		}
		managementKey, err := os.ReadFile(keyPath)
		if err != nil {
			if os.IsNotExist(err) {
				return settings, fmt.Errorf("%w: %v", ErrMoldDBPasswordNotConfigured, err)
			}
			return settings, fmt.Errorf("read Mold management key: %w", err)
		}
		password, err = decryptCloudStackV2(
			strings.TrimSuffix(strings.TrimPrefix(passwordValue, "ENC("), ")"),
			strings.TrimSpace(string(managementKey)),
		)
		if err != nil {
			return settings, fmt.Errorf("decrypt Mold database password: %w", err)
		}
	}

	port := 3306
	if value := strings.TrimSpace(properties["db.cloud.port"]); value != "" {
		port, err = strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return settings, fmt.Errorf("invalid Mold database port %q", value)
		}
	}
	settings.Config = MoldDBConfig{
		Host: strings.TrimSpace(properties["db.cloud.host"]),
		Port: port,
		Name: strings.TrimSpace(properties["db.cloud.name"]),
		User: strings.TrimSpace(properties["db.cloud.username"]),
	}
	if settings.Config.Host == "" || settings.Config.Name == "" || settings.Config.User == "" {
		return moldDBSettings{}, fmt.Errorf("Mold database connection properties are incomplete")
	}
	settings.Password = password
	return settings, nil
}

func decryptCloudStackV2(encoded, managementKey string) (string, error) {
	if managementKey == "" {
		return "", fmt.Errorf("Mold management key is empty")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("invalid Base64 ciphertext: %w", err)
	}
	if len(ciphertext) < cloudStackV2NonceSize+16 {
		return "", fmt.Errorf("encrypted value is too short")
	}
	key := sha256.Sum256([]byte(managementKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, ciphertext[:cloudStackV2NonceSize], ciphertext[cloudStackV2NonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}
	return string(plaintext), nil
}

// readJavaProperties reads the simple key/value subset used by CloudStack's
// generated db.properties. It supports comments, whitespace and =/: separators.
func readJavaProperties(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	properties := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		separator := strings.IndexAny(line, "=:")
		if separator < 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if key != "" {
			properties[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return properties, nil
}
