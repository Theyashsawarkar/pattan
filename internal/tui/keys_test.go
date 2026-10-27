package tui

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite scripts/keybindings")

// scripts/keybindings (Vayu's keybinding search) must match Keys.
func TestKeybindingsFile(t *testing.T) {
	const path = "../../scripts/keybindings"
	want := KeybindingsFile()
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatal("scripts/keybindings is out of date: go test ./internal/tui -update")
	}
}

// Every key reads verb-first, as the keybinding search wants.
func TestKeysVerbFirst(t *testing.T) {
	verbs := "move jump switch show search rescan quit open copy start stop force send"
	for _, k := range Keys {
		v := strings.ToLower(strings.Fields(k.Desc)[0])
		if !strings.Contains(" "+verbs+" ", " "+v+" ") {
			t.Errorf("%s: %q doesn't start with a verb", k.Keys, k.Desc)
		}
	}
}
