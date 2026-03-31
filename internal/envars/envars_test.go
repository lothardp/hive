package envars

import (
	"testing"
)

func TestBuildVarsPortsOnly(t *testing.T) {
	ports := map[string]int{"PORT": 3001, "DB_PORT": 5433}
	vars := BuildVars(ports, nil)

	if vars["PORT"] != "3001" {
		t.Errorf("expected PORT=3001, got %s", vars["PORT"])
	}
	if vars["DB_PORT"] != "5433" {
		t.Errorf("expected DB_PORT=5433, got %s", vars["DB_PORT"])
	}
	if len(vars) != 2 {
		t.Errorf("expected 2 vars, got %d", len(vars))
	}
}

func TestBuildVarsStaticOnly(t *testing.T) {
	static := map[string]string{"NODE_ENV": "development", "LOG_LEVEL": "debug"}
	vars := BuildVars(nil, static)

	if vars["NODE_ENV"] != "development" {
		t.Errorf("expected NODE_ENV=development, got %s", vars["NODE_ENV"])
	}
	if vars["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL=debug, got %s", vars["LOG_LEVEL"])
	}
	if len(vars) != 2 {
		t.Errorf("expected 2 vars, got %d", len(vars))
	}
}

func TestBuildVarsCombined(t *testing.T) {
	ports := map[string]int{"PORT": 3001}
	static := map[string]string{"NODE_ENV": "development"}
	vars := BuildVars(ports, static)

	if len(vars) != 2 {
		t.Errorf("expected 2 vars, got %d", len(vars))
	}
	if vars["PORT"] != "3001" {
		t.Errorf("expected PORT=3001, got %s", vars["PORT"])
	}
	if vars["NODE_ENV"] != "development" {
		t.Errorf("expected NODE_ENV=development, got %s", vars["NODE_ENV"])
	}
}

func TestBuildVarsPortsPrecedence(t *testing.T) {
	ports := map[string]int{"PORT": 3001}
	static := map[string]string{"PORT": "8080"}
	vars := BuildVars(ports, static)

	if vars["PORT"] != "3001" {
		t.Errorf("expected port to take precedence, got PORT=%s", vars["PORT"])
	}
}

func TestBuildVarsEmpty(t *testing.T) {
	vars := BuildVars(nil, nil)
	if len(vars) != 0 {
		t.Errorf("expected empty map, got %v", vars)
	}
}
