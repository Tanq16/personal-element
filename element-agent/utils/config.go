package utils

import (
	"errors"
	"os"
	"path/filepath"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

const appName = "element-agent"

var ErrNoToken = errors.New("no server credentials configured")

type Token struct {
	ServerURL string `json:"server_url"`
	Secret    string `json:"secret"`
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appName), nil
}

func tokenPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

func LoadToken() (*Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if t.ServerURL == "" || t.Secret == "" {
		return nil, ErrNoToken
	}
	return &t, nil
}

func SaveToken(t *Token) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(t, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "token.json")
	return os.WriteFile(path, data, 0o600)
}
