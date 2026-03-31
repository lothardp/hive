package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectConfig struct {
	ComposePath string            `yaml:"compose_path" json:"compose_path"`
	SeedScripts []string          `yaml:"seed_scripts" json:"seed_scripts"`
	ExposePort  int               `yaml:"expose_port" json:"expose_port"`
	Env         map[string]string `yaml:"env" json:"env"`
	Hooks       []string          `yaml:"hooks" json:"hooks"`
	Layouts     map[string]Layout `yaml:"layouts" json:"layouts"`
	PortVars    []string          `yaml:"port_vars" json:"port_vars"`
}

type Layout struct {
	Windows []Window `yaml:"windows" json:"windows"`
}

type Window struct {
	Name  string `yaml:"name" json:"name"`
	Panes []Pane `yaml:"panes" json:"panes"`
}

type Pane struct {
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	Split   string `yaml:"split,omitempty" json:"split,omitempty"`
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

func (c *ProjectConfig) ToJSON() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshaling config to JSON: %w", err)
	}
	return string(data), nil
}

func ProjectConfigFromJSON(data string) (*ProjectConfig, error) {
	var cfg ProjectConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config JSON: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *ProjectConfig) ToYAML() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling config to YAML: %w", err)
	}
	return data, nil
}
