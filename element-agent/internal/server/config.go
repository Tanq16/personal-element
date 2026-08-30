package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Homeserver     string        `yaml:"homeserver"`
	ServerName     string        `yaml:"server_name"`
	Listen         string        `yaml:"listen"`
	ClientPrefix   string        `yaml:"client_prefix"`
	StatePath      string        `yaml:"state_path"`
	ReconcileEvery time.Duration `yaml:"reconcile_every"`
	BackfillLimit  int           `yaml:"backfill_limit"`

	ASToken      string `yaml:"-"`
	HSToken      string `yaml:"-"`
	AdminToken   string `yaml:"-"`
	SharedSecret string `yaml:"-"`
}

func defaults() Config {
	return Config{
		Homeserver:     "http://127.0.0.1:8008",
		Listen:         "127.0.0.1:9000",
		StatePath:      "/data/state.json",
		ReconcileEvery: 5 * time.Minute,
		BackfillLimit:  10,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	}

	cfg.ClientPrefix = "/" + strings.Trim(cfg.ClientPrefix, "/")
	if err := cfg.loadTokens(); err != nil {
		return cfg, err
	}
	return cfg, cfg.validate()
}

func (c *Config) loadTokens() error {
	vars := []struct {
		name   string
		target *string
	}{
		{"ELEMENT_AGENT_AS_TOKEN", &c.ASToken},
		{"ELEMENT_AGENT_HS_TOKEN", &c.HSToken},
		{"ELEMENT_AGENT_ADMIN_TOKEN", &c.AdminToken},
		{"ELEMENT_AGENT_SHARED_SECRET", &c.SharedSecret},
	}
	var missing []string
	for _, v := range vars {
		value := os.Getenv(v.name)
		if value == "" {
			missing = append(missing, v.name)
			continue
		}
		*v.target = value
	}
	if len(missing) > 0 {
		return fmt.Errorf("unset in the environment: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *Config) validate() error {
	if c.ServerName == "" {
		return errors.New("server_name is not set")
	}
	if c.ClientPrefix == "/" {
		return errors.New("client_prefix is not set")
	}
	if c.BackfillLimit < 1 {
		return errors.New("backfill_limit must be at least 1")
	}
	if c.ReconcileEvery < time.Second {
		return errors.New("reconcile_every must be at least 1s")
	}
	return nil
}
