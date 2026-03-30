package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectConfig struct {
	ComposePath string            `yaml:"compose_path"`
	SeedScripts []string          `yaml:"seed_scripts"`
	ExposePort  int               `yaml:"expose_port"`
	Env         map[string]string `yaml:"env"`
}

func Load(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".hive.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func LoadOrDefault(dir string) *ProjectConfig {
	cfg, err := Load(dir)
	if err != nil {
		cfg = &ProjectConfig{}
		cfg.applyDefaults()
	}
	return cfg
}

func (c *ProjectConfig) applyDefaults() {
	if c.ComposePath == "" {
		c.ComposePath = "docker-compose.yml"
	}
	if c.Env == nil {
		c.Env = make(map[string]string)
	}
}
