// pattan: the ports your dev servers listen on, Docker's published ports
// and the tunnels that make them public, in the terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Theyashsawarkar/pattan/internal/scan"
	"github.com/Theyashsawarkar/pattan/internal/tui"
)

// Version is pattan's version.
const Version = "0.1.1"

const usage = `pattan: the ports your dev servers listen on, and their tunnels.

  pattan                 the screen (? in it lists every key)
  pattan list            the dev servers' ports, one per line: "5173 vite ~/code/app"
  pattan list --all      every listening port
  pattan tunnels         the tunnels running now: "ngrok https://… -> :5173"
  pattan --popup KEY     run inside a tmux popup: KEY closes it too
  pattan --version

Keys, the tmux plugin and its options: https://github.com/Theyashsawarkar/pattan
`

func main() {
	fs := flag.NewFlagSet("pattan", flag.ExitOnError)
	popup := fs.String("popup", "", "")
	client := fs.String("client", os.Getenv("PATTAN_CLIENT"), "")
	all := fs.Bool("all", false, "")
	version := fs.Bool("version", false, "")
	fs.Usage = func() { fmt.Print(usage) }
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	switch {
	case *version:
		fmt.Println("pattan", Version)
		return
	case cmd == "list":
		list(*all)
		return
	case cmd == "tunnels":
		tunnels()
		return
	case cmd == "help":
		fmt.Print(usage)
		return
	case cmd == "keybindings": // scripts/keybindings, for Vayu's keybinding search
		fmt.Print(tui.KeybindingsFile())
		return
	case cmd != "":
		fmt.Fprintf(os.Stderr, "pattan: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	p := tea.NewProgram(tui.New(tui.Options{PopupKey: *popup, Client: *client}), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "pattan:", err)
		os.Exit(1)
	}
}

func list(all bool) {
	ls, err := (&scan.Scanner{}).Listening(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pattan:", err)
		os.Exit(1)
	}
	for _, l := range ls {
		if !all && !l.Dev {
			continue
		}
		where := l.Project
		if l.PID == 0 {
			where = "(another user's)"
		}
		fmt.Printf("%d %s %s\n", l.Port, orDash(l.App), orDash(where))
	}
}

func tunnels() {
	ctx := context.Background()
	ls, _ := (&scan.Scanner{}).Listening(ctx)
	for _, t := range scan.Tunnels(ctx, ls, scan.Procs(), true) {
		pub := t.Public
		if pub == "" {
			pub = "(" + t.Note + ")"
		}
		fmt.Printf("%s %s -> :%d\n", t.Tool, pub, t.Port)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
