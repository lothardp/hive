package keybindings

import (
	"strings"
	"testing"
)

func TestGenerateDefaultLeader(t *testing.T) {
	content := Generate("H", 3.4)

	if !strings.Contains(content, "bind-key H switch-client -T hive") {
		t.Error("expected leader key H in key table entry")
	}
	if !strings.Contains(content, "bind-key -T hive s display-popup") {
		t.Error("expected switch binding")
	}
	if !strings.Contains(content, "bind-key -T hive c display-popup") {
		t.Error("expected create binding")
	}
	if !strings.Contains(content, "bind-key -T hive k display-popup") {
		t.Error("expected kill binding")
	}
	if !strings.Contains(content, "bind-key -T hive d display-popup") {
		t.Error("expected dashboard binding")
	}
	if !strings.Contains(content, "hive switch") {
		t.Error("expected hive switch command in switch binding")
	}
	if !strings.Contains(content, "hive kill") {
		t.Error("expected hive kill command in kill binding")
	}
	if !strings.Contains(content, "hive status") {
		t.Error("expected hive status command in dashboard binding")
	}
}

func TestGenerateCustomLeader(t *testing.T) {
	content := Generate("F", 3.4)

	if !strings.Contains(content, "bind-key F switch-client -T hive") {
		t.Error("expected custom leader key F")
	}
	if strings.Contains(content, "bind-key H switch-client") {
		t.Error("should not contain default leader H")
	}
	if !strings.Contains(content, "<prefix> F, then:") {
		t.Error("expected comment to reference custom leader")
	}
}

func TestGenerateOldTmuxVersion(t *testing.T) {
	content := Generate("H", 3.1)

	if !strings.Contains(content, "WARNING: tmux 3.2+ required") {
		t.Error("expected version warning")
	}
	if !strings.Contains(content, "3.1") {
		t.Error("expected current version in warning")
	}
	// All bindings should be commented out
	if strings.Contains(content, "\nbind-key") {
		t.Error("bindings should be commented out for old tmux")
	}
	if !strings.Contains(content, "# bind-key H switch-client -T hive") {
		t.Error("expected commented-out leader binding")
	}
}

func TestGenerateUnknownVersion(t *testing.T) {
	// Version 0 means unknown — generate normally
	content := Generate("H", 0)

	if strings.Contains(content, "WARNING") {
		t.Error("should not warn for unknown version")
	}
	if !strings.Contains(content, "bind-key H switch-client -T hive") {
		t.Error("expected active bindings for unknown version")
	}
}

func TestGenerateManagedHeader(t *testing.T) {
	content := Generate("H", 3.4)

	if !strings.Contains(content, "This file is managed by Hive") {
		t.Error("expected managed-by header")
	}
	if !strings.Contains(content, "hive keybindings") {
		t.Error("expected regenerate instruction in header")
	}
}

func TestParseTmuxVersionNormal(t *testing.T) {
	v, err := ParseTmuxVersion("tmux 3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 3.4 {
		t.Errorf("expected 3.4, got %f", v)
	}
}

func TestParseTmuxVersionWithSuffix(t *testing.T) {
	v, err := ParseTmuxVersion("tmux 3.2a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 3.2 {
		t.Errorf("expected 3.2, got %f", v)
	}
}

func TestParseTmuxVersionNext(t *testing.T) {
	v, err := ParseTmuxVersion("tmux next-3.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 3.5 {
		t.Errorf("expected 3.5, got %f", v)
	}
}

func TestParseTmuxVersionInvalid(t *testing.T) {
	_, err := ParseTmuxVersion("not tmux")
	if err == nil {
		t.Error("expected error for invalid version string")
	}
}

func TestBindings(t *testing.T) {
	bindings := Bindings("H")
	if len(bindings) != 4 {
		t.Fatalf("expected 4 bindings, got %d", len(bindings))
	}
	if !strings.Contains(bindings[0], "<prefix> H s") {
		t.Errorf("unexpected first binding: %s", bindings[0])
	}
}

func TestBindingsCustomLeader(t *testing.T) {
	bindings := Bindings("F")
	for _, b := range bindings {
		if !strings.Contains(b, "<prefix> F") {
			t.Errorf("expected custom leader in binding: %s", b)
		}
	}
}
