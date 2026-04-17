package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// GlobalConfig is loaded from ~/.hive/config.yaml
type GlobalConfig struct {
	ProjectDirs   []string `yaml:"project_dirs"`
	CellsDir      string   `yaml:"cells_dir"`
	MulticellsDir string   `yaml:"multicells_dir"`
	Editor        string   `yaml:"editor"`
	TmuxLeader    string   `yaml:"tmux_leader"`
}

// ProjectConfig is loaded from ~/.hive/config/{project}.yml
type ProjectConfig struct {
	RepoPath string            `yaml:"repo_path"`
	Hooks    []string          `yaml:"hooks"`
	Env      map[string]string `yaml:"env"`
	PortVars []string          `yaml:"port_vars"`
	Layouts  map[string]Layout `yaml:"layouts"`
}

type Layout struct {
	Windows []Window `yaml:"windows"`
}

type Window struct {
	Name  string `yaml:"name"`
	Panes []Pane `yaml:"panes"`
}

type Pane struct {
	Command string `yaml:"command,omitempty"`
	Split   string `yaml:"split,omitempty"`
}

// DiscoveredProject represents a git repo found inside a project directory.
type DiscoveredProject struct {
	Name string // directory basename
	Path string // absolute path to the repo
}

// ---------------------------------------------------------------------------
// GlobalConfig loaders
// ---------------------------------------------------------------------------

// LoadGlobal reads the global config from {hiveDir}/config.yaml.
func LoadGlobal(hiveDir string) (*GlobalConfig, error) {
	path := filepath.Join(hiveDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// LoadGlobalOrDefault loads the global config, falling back to sensible
// defaults if the file doesn't exist or can't be parsed.
func LoadGlobalOrDefault(hiveDir string) *GlobalConfig {
	cfg, err := LoadGlobal(hiveDir)
	if err != nil {
		cfg = &GlobalConfig{
			CellsDir:      "~/hive/cells",
			MulticellsDir: "~/hive/multicells",
			Editor:        "vim",
			TmuxLeader:    "C-a",
		}
	}
	return cfg
}

// ---------------------------------------------------------------------------
// ProjectConfig loaders
// ---------------------------------------------------------------------------

// LoadProject reads the project config from {hiveDir}/config/{project}.yml.
func LoadProject(hiveDir, project string) (*ProjectConfig, error) {
	path := filepath.Join(hiveDir, "config", project+".yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// LoadProjectOrDefault loads the project config, falling back to an empty
// config if the file doesn't exist or can't be parsed.
func LoadProjectOrDefault(hiveDir, project string) *ProjectConfig {
	cfg, err := LoadProject(hiveDir, project)
	if err != nil {
		cfg = &ProjectConfig{}
	}
	return cfg
}

// ---------------------------------------------------------------------------
// Writers
// ---------------------------------------------------------------------------

// WriteDefaultGlobal writes the global config to {hiveDir}/config.yaml,
// creating the directory if needed.
func WriteDefaultGlobal(hiveDir string, cfg *GlobalConfig) error {
	if err := os.MkdirAll(hiveDir, 0o755); err != nil {
		return fmt.Errorf("creating hive dir %s: %w", hiveDir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling global config: %w", err)
	}
	path := filepath.Join(hiveDir, "config.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WriteDefaultProject writes the project config to {hiveDir}/config/{project}.yml,
// creating the config/ directory if needed.
func WriteDefaultProject(hiveDir, project string, cfg *ProjectConfig) error {
	configDir := filepath.Join(hiveDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", configDir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling project config: %w", err)
	}
	path := filepath.Join(configDir, project+".yml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resolvers
// ---------------------------------------------------------------------------

// ResolveProjectDirs expands ~ to the user's home directory in each entry.
func (g *GlobalConfig) ResolveProjectDirs() []string {
	out := make([]string, len(g.ProjectDirs))
	for i, d := range g.ProjectDirs {
		out[i] = expandTilde(d)
	}
	return out
}

// ResolveCellsDir expands ~ to the user's home directory.
func (g *GlobalConfig) ResolveCellsDir() string {
	return expandTilde(g.CellsDir)
}

// ResolveMulticellsDir expands ~ to the user's home directory. Falls back to
// ~/hive/multicells when unset.
func (g *GlobalConfig) ResolveMulticellsDir() string {
	if g.MulticellsDir == "" {
		return expandTilde("~/hive/multicells")
	}
	return expandTilde(g.MulticellsDir)
}

// ResolveEditor returns the configured editor, falling back to the $EDITOR
// environment variable, then to "vim".
func (g *GlobalConfig) ResolveEditor() string {
	if g.Editor != "" {
		return g.Editor
	}
	if env := os.Getenv("EDITOR"); env != "" {
		return env
	}
	return "vim"
}

// ---------------------------------------------------------------------------
// Project discovery
// ---------------------------------------------------------------------------

// DiscoverProjects scans each directory in projectDirs one level deep and
// returns entries that contain a .git file or directory. Results are sorted
// alphabetically by name.
func DiscoverProjects(projectDirs []string) ([]DiscoveredProject, error) {
	var projects []DiscoveredProject
	seen := make(map[string]bool)

	for _, dir := range projectDirs {
		dir = expandTilde(dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Skip directories that don't exist or can't be read.
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			absPath := filepath.Join(dir, entry.Name())
			// Check for .git (file or directory).
			gitPath := filepath.Join(absPath, ".git")
			if _, err := os.Stat(gitPath); err != nil {
				continue
			}
			name := entry.Name()
			if seen[name] {
				continue
			}
			seen[name] = true
			projects = append(projects, DiscoveredProject{
				Name: name,
				Path: absPath,
			})
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	return projects, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
