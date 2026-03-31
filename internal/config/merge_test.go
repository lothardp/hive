package config

import (
	"testing"
)

func TestMerge_ScalarOverwrite(t *testing.T) {
	base := &ProjectConfig{
		ComposePath: "docker-compose.yml",
		ExposePort:  3000,
	}
	other := &ProjectConfig{
		ComposePath: "compose.yaml",
		ExposePort:  4000,
	}
	base.Merge(other)

	if base.ComposePath != "compose.yaml" {
		t.Errorf("ComposePath = %q, want %q", base.ComposePath, "compose.yaml")
	}
	if base.ExposePort != 4000 {
		t.Errorf("ExposePort = %d, want %d", base.ExposePort, 4000)
	}
}

func TestMerge_ScalarZeroNoOverwrite(t *testing.T) {
	base := &ProjectConfig{
		ComposePath: "docker-compose.yml",
		ExposePort:  3000,
	}
	other := &ProjectConfig{} // zero values
	base.Merge(other)

	if base.ComposePath != "docker-compose.yml" {
		t.Errorf("ComposePath should not be overwritten by zero value")
	}
	if base.ExposePort != 3000 {
		t.Errorf("ExposePort should not be overwritten by zero value")
	}
}

func TestMerge_ListsReplace(t *testing.T) {
	base := &ProjectConfig{
		Hooks:       []string{"echo a", "echo b"},
		SeedScripts: []string{"seed1.sql"},
	}
	other := &ProjectConfig{
		Hooks:       []string{"echo c"},
		SeedScripts: []string{"seed2.sql", "seed3.sql"},
	}
	base.Merge(other)

	if len(base.Hooks) != 1 || base.Hooks[0] != "echo c" {
		t.Errorf("Hooks = %v, want [echo c]", base.Hooks)
	}
	if len(base.SeedScripts) != 2 {
		t.Errorf("SeedScripts = %v, want 2 items", base.SeedScripts)
	}
}

func TestMerge_NilListNoOverwrite(t *testing.T) {
	base := &ProjectConfig{
		Hooks: []string{"echo a"},
	}
	other := &ProjectConfig{} // nil hooks
	base.Merge(other)

	if len(base.Hooks) != 1 {
		t.Errorf("Hooks should not be replaced by nil")
	}
}

func TestMerge_MapsMerge(t *testing.T) {
	base := &ProjectConfig{
		Env: map[string]string{"A": "1", "B": "2"},
		Layouts: map[string]Layout{
			"default": {Windows: []Window{{Name: "editor"}}},
		},
	}
	other := &ProjectConfig{
		Env: map[string]string{"B": "3", "C": "4"},
		Layouts: map[string]Layout{
			"minimal": {Windows: []Window{{Name: "shell"}}},
		},
	}
	base.Merge(other)

	if base.Env["A"] != "1" {
		t.Errorf("Env[A] should be preserved")
	}
	if base.Env["B"] != "3" {
		t.Errorf("Env[B] = %q, want %q", base.Env["B"], "3")
	}
	if base.Env["C"] != "4" {
		t.Errorf("Env[C] = %q, want %q", base.Env["C"], "4")
	}
	if _, ok := base.Layouts["default"]; !ok {
		t.Error("Layouts[default] should be preserved")
	}
	if _, ok := base.Layouts["minimal"]; !ok {
		t.Error("Layouts[minimal] should be added")
	}
}

func TestMerge_MapsInitFromNil(t *testing.T) {
	base := &ProjectConfig{}
	other := &ProjectConfig{
		Env:     map[string]string{"A": "1"},
		Layouts: map[string]Layout{"default": {}},
	}
	base.Merge(other)

	if base.Env["A"] != "1" {
		t.Error("Env should be initialized and merged")
	}
	if _, ok := base.Layouts["default"]; !ok {
		t.Error("Layouts should be initialized and merged")
	}
}
