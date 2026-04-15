package keybindings

import (
	"strings"
	"testing"
)

func TestGenerateTmuxConf(t *testing.T) {
	conf := GenerateTmuxConf()
	if !strings.Contains(conf, "switch-client -t hive") {
		t.Errorf("expected switch-client binding, got: %s", conf)
	}
}
