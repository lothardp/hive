package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobal(t *testing.T) {
	dir := t.TempDir()
	yaml := `project_dirs:
  - ~/projects
  - /opt/repos
cells_dir: ~/hive/cells
editor: nvim
tmux_leader: C-b
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobal(dir)
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	if len(cfg.ProjectDirs) != 2 {
		t.Fatalf("ProjectDirs len = %d, want 2", len(cfg.ProjectDirs))
	}
	if cfg.ProjectDirs[0] != "~/projects" {
		t.Errorf("ProjectDirs[0] = %q, want %q", cfg.ProjectDirs[0], "~/projects")
	}
	if cfg.CellsDir != "~/hive/cells" {
		t.Errorf("CellsDir = %q, want %q", cfg.CellsDir, "~/hive/cells")
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", cfg.Editor, "nvim")
	}
	if cfg.TmuxLeader != "C-b" {
		t.Errorf("TmuxLeader = %q, want %q", cfg.TmuxLeader, "C-b")
	}
}

func TestLoadGlobal_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadGlobal(dir)
	if err == nil {
		t.Fatal("LoadGlobal should return error for missing file")
	}
}

func TestLoadGlobalOrDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := LoadGlobalOrDefault(dir)

	if cfg.CellsDir != "~/hive/cells" {
		t.Errorf("default CellsDir = %q, want %q", cfg.CellsDir, "~/hive/cells")
	}
	if cfg.Editor != "vim" {
		t.Errorf("default Editor = %q, want %q", cfg.Editor, "vim")
	}
	if cfg.TmuxLeader != "C-a" {
		t.Errorf("default TmuxLeader = %q, want %q", cfg.TmuxLeader, "C-a")
	}
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.Mkdir(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `repo_path: /home/user/projects/myapp
hooks:
  - npm install
  - npm run build
env:
  NODE_ENV: development
  DEBUG: "true"
port_vars:
  - PORT
  - DB_PORT
layouts:
  default:
    windows:
      - name: editor
        panes:
          - command: vim .
      - name: server
        panes:
          - command: npm start
          - command: npm run watch
            split: horizontal
`
	if err := os.WriteFile(filepath.Join(configDir, "myapp.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProject(dir, "myapp")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	if cfg.RepoPath != "/home/user/projects/myapp" {
		t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, "/home/user/projects/myapp")
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("Hooks len = %d, want 2", len(cfg.Hooks))
	}
	if cfg.Hooks[0] != "npm install" {
		t.Errorf("Hooks[0] = %q, want %q", cfg.Hooks[0], "npm install")
	}
	if cfg.Env["NODE_ENV"] != "development" {
		t.Errorf("Env[NODE_ENV] = %q, want %q", cfg.Env["NODE_ENV"], "development")
	}
	if len(cfg.PortVars) != 2 {
		t.Fatalf("PortVars len = %d, want 2", len(cfg.PortVars))
	}
	layout, ok := cfg.Layouts["default"]
	if !ok {
		t.Fatal("Layouts[default] missing")
	}
	if len(layout.Windows) != 2 {
		t.Fatalf("Windows len = %d, want 2", len(layout.Windows))
	}
	if layout.Windows[0].Name != "editor" {
		t.Errorf("Windows[0].Name = %q, want %q", layout.Windows[0].Name, "editor")
	}
	if layout.Windows[1].Panes[1].Split != "horizontal" {
		t.Errorf("Windows[1].Panes[1].Split = %q, want %q", layout.Windows[1].Panes[1].Split, "horizontal")
	}
}

func TestLoadProject_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadProject(dir, "nonexistent")
	if err == nil {
		t.Fatal("LoadProject should return error for missing file")
	}
}

func TestLoadProjectOrDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := LoadProjectOrDefault(dir, "nonexistent")

	if cfg.RepoPath != "" {
		t.Errorf("default RepoPath should be empty, got %q", cfg.RepoPath)
	}
	if cfg.Hooks != nil {
		t.Errorf("default Hooks should be nil, got %v", cfg.Hooks)
	}
	if cfg.Env != nil {
		t.Errorf("default Env should be nil, got %v", cfg.Env)
	}
}

func TestWriteDefaultGlobal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "hive")
	cfg := &GlobalConfig{
		ProjectDirs: []string{"~/projects"},
		CellsDir:    "~/hive/cells",
		Editor:       "code",
		TmuxLeader:   "C-a",
	}

	if err := WriteDefaultGlobal(dir, cfg); err != nil {
		t.Fatalf("WriteDefaultGlobal: %v", err)
	}

	// Read it back.
	loaded, err := LoadGlobal(dir)
	if err != nil {
		t.Fatalf("LoadGlobal after write: %v", err)
	}
	if loaded.Editor != "code" {
		t.Errorf("Editor = %q, want %q", loaded.Editor, "code")
	}
	if len(loaded.ProjectDirs) != 1 || loaded.ProjectDirs[0] != "~/projects" {
		t.Errorf("ProjectDirs = %v, want [~/projects]", loaded.ProjectDirs)
	}
}

func TestWriteDefaultProject(t *testing.T) {
	dir := t.TempDir()
	cfg := &ProjectConfig{
		RepoPath: "/tmp/myapp",
		Hooks:    []string{"make build"},
		Env:      map[string]string{"FOO": "bar"},
	}

	if err := WriteDefaultProject(dir, "myapp", cfg); err != nil {
		t.Fatalf("WriteDefaultProject: %v", err)
	}

	loaded, err := LoadProject(dir, "myapp")
	if err != nil {
		t.Fatalf("LoadProject after write: %v", err)
	}
	if loaded.RepoPath != "/tmp/myapp" {
		t.Errorf("RepoPath = %q, want %q", loaded.RepoPath, "/tmp/myapp")
	}
	if loaded.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", loaded.Env["FOO"], "bar")
	}
}

func TestResolveCellsDir_TildeExpansion(t *testing.T) {
	cfg := &GlobalConfig{CellsDir: "~/hive/cells"}
	resolved := cfg.ResolveCellsDir()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(home, "hive/cells")
	if resolved != want {
		t.Errorf("ResolveCellsDir = %q, want %q", resolved, want)
	}
}

func TestResolveCellsDir_AbsolutePath(t *testing.T) {
	cfg := &GlobalConfig{CellsDir: "/opt/hive/cells"}
	resolved := cfg.ResolveCellsDir()

	if resolved != "/opt/hive/cells" {
		t.Errorf("ResolveCellsDir = %q, want %q", resolved, "/opt/hive/cells")
	}
}

func TestResolveProjectDirs_TildeExpansion(t *testing.T) {
	cfg := &GlobalConfig{ProjectDirs: []string{"~/projects", "/opt/repos"}}
	resolved := cfg.ResolveProjectDirs()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	if resolved[0] != filepath.Join(home, "projects") {
		t.Errorf("ResolveProjectDirs[0] = %q, want %q", resolved[0], filepath.Join(home, "projects"))
	}
	if resolved[1] != "/opt/repos" {
		t.Errorf("ResolveProjectDirs[1] = %q, want %q", resolved[1], "/opt/repos")
	}
}

func TestResolveEditor(t *testing.T) {
	// With editor set.
	cfg := &GlobalConfig{Editor: "nvim"}
	if got := cfg.ResolveEditor(); got != "nvim" {
		t.Errorf("ResolveEditor = %q, want %q", got, "nvim")
	}

	// With empty editor, falls back to $EDITOR.
	cfg = &GlobalConfig{}
	t.Setenv("EDITOR", "emacs")
	if got := cfg.ResolveEditor(); got != "emacs" {
		t.Errorf("ResolveEditor = %q, want %q", got, "emacs")
	}

	// With empty editor and no $EDITOR, falls back to vim.
	t.Setenv("EDITOR", "")
	if got := cfg.ResolveEditor(); got != "vim" {
		t.Errorf("ResolveEditor = %q, want %q", got, "vim")
	}
}

func TestDiscoverProjects(t *testing.T) {
	base := t.TempDir()

	// Create three directories: two with .git, one without.
	for _, name := range []string{"beta-app", "alpha-app"} {
		repoDir := filepath.Join(base, name)
		if err := os.Mkdir(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Create .git directory.
		if err := os.Mkdir(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a .git file (worktree-style) for a third project.
	worktreeDir := filepath.Join(base, "charlie-wt")
	if err := os.Mkdir(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: /somewhere/.git/worktrees/charlie-wt"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a non-git directory.
	if err := os.Mkdir(filepath.Join(base, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	projects, err := DiscoverProjects([]string{base})
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}

	if len(projects) != 3 {
		t.Fatalf("got %d projects, want 3", len(projects))
	}

	// Should be sorted alphabetically.
	if projects[0].Name != "alpha-app" {
		t.Errorf("projects[0].Name = %q, want %q", projects[0].Name, "alpha-app")
	}
	if projects[1].Name != "beta-app" {
		t.Errorf("projects[1].Name = %q, want %q", projects[1].Name, "beta-app")
	}
	if projects[2].Name != "charlie-wt" {
		t.Errorf("projects[2].Name = %q, want %q", projects[2].Name, "charlie-wt")
	}

	// Check absolute paths.
	if projects[0].Path != filepath.Join(base, "alpha-app") {
		t.Errorf("projects[0].Path = %q, want %q", projects[0].Path, filepath.Join(base, "alpha-app"))
	}
}

func TestDiscoverProjects_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	repoA := filepath.Join(dir1, "aaa")
	repoB := filepath.Join(dir2, "bbb")
	for _, d := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := DiscoverProjects([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	if projects[0].Name != "aaa" {
		t.Errorf("projects[0].Name = %q, want %q", projects[0].Name, "aaa")
	}
	if projects[1].Name != "bbb" {
		t.Errorf("projects[1].Name = %q, want %q", projects[1].Name, "bbb")
	}
}

func TestDiscoverProjects_DeduplicatesByName(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Same name in two different directories.
	for _, d := range []string{dir1, dir2} {
		repo := filepath.Join(d, "samename")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := DiscoverProjects([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1 (deduplication)", len(projects))
	}
}

func TestDiscoverProjects_NonExistentDir(t *testing.T) {
	projects, err := DiscoverProjects([]string{"/nonexistent/path/12345"})
	if err != nil {
		t.Fatalf("DiscoverProjects should not error for missing dirs: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects, want 0 for missing dir", len(projects))
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		got := expandTilde(tt.input)
		if got != tt.want {
			t.Errorf("expandTilde(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
