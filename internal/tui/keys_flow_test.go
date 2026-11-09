package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Theyashsawarkar/pattan/internal/scan"
)

func press(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	return m
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestPaletteFilters(t *testing.T) {
	m := New(Options{})
	m.w, m.h = 150, 40
	m = press(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if m.pop != popPalette {
		t.Fatalf("Space Space: pop %v, pending %q", m.pop, m.eng.Pending())
	}
	m = press(m, runes("r"), runes("e"), runes("s"))
	if m.pal.Query != "res" {
		t.Fatalf("query %q", m.pal.Query)
	}
}

// A fast burst arrives as one message ("gg", "3j"): its keys still count
// one by one, and a confirm that opens drops the rest, so "ddy" typed at
// once asks and never stops anything.
func TestBurstsAreKeys(t *testing.T) {
	m := New(Options{})
	m.w, m.h = 150, 40
	m.tab = tabAll
	for p := 1; p <= 5; p++ {
		m.ls = append(m.ls, scan.Listener{Port: 3000 + p, PID: 999999, Proc: "fake"})
	}
	m = press(m, runes("3j"))
	if m.sel[tabAll] != 3 {
		t.Fatalf("3j burst: sel %d, want 3", m.sel[tabAll])
	}
	m = press(m, runes("gg"))
	if m.sel[tabAll] != 0 {
		t.Fatalf("gg burst: sel %d, want 0", m.sel[tabAll])
	}
	// The confirm's commands are never run here: y would have closed it.
	m = press(m, runes("ddy"))
	if m.pop != popConfirm {
		t.Fatalf("ddy burst: pop %v, want the confirm still open", m.pop)
	}
	m = press(m, runes("n"))
	if m.pop != popNone {
		t.Fatalf("n: pop %v, want closed", m.pop)
	}
	m = press(m, runes("/ab"))
	if !m.filtering || m.filter.Value() != "ab" {
		t.Fatalf("/ab burst: filtering %v, filter %q", m.filtering, m.filter.Value())
	}
}

// In a tmux popup, its key closes pattan from the help sheet too.
func TestPopupKeyClosesFromHelp(t *testing.T) {
	m := New(Options{PopupKey: "P"})
	m.w, m.h = 150, 40
	m = press(m, runes("?"))
	if m.pop != popHelp {
		t.Fatalf("?: pop %v", m.pop)
	}
	_, cmd := m.Update(runes("P"))
	if cmd == nil {
		t.Fatal("P in the help: no command, want quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("P in the help didn't quit")
	}
}
