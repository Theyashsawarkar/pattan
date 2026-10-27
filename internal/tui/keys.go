package tui

import (
	"fmt"
	"strings"
)

// Key is one key of the screen: what you press, what it does, and where
// (empty: everywhere).
type Key struct {
	Keys, Desc, Where string
}

// Keys is every key, in the order the ? sheet lists them. The README's key
// table and scripts/keybindings (Vayu's keybinding search) come from it:
// after a change run `go test ./internal/tui -update`.
var Keys = []Key{
	{"j / k", "Move to the next / previous port", ""},
	{"Home / End", "Jump to the first / last port", ""},
	{"Tab / Shift+Tab", "Switch to the next / previous tab", ""},
	{"1 – 4", "Switch to Dev servers, All listening, Tunnels or Docker", ""},
	{"a", "Show every listening port (the All listening tab)", ""},
	{"/", "Search the list by port, app, project or URL (filters it)", ""},
	{"r", "Rescan the ports now (it also rescans every 2 s)", ""},
	{"?", "Show every key", ""},
	{"q / Esc", "Quit (Esc clears the filter first)", ""},
	{"Enter", "Open localhost:port in the browser", "ports"},
	{"y", "Copy the URL", "ports"},
	{"Y", "Copy the URL other machines on your network can use", "ports"},
	{"g", "Jump to the tmux pane the server runs in", "ports"},
	{"t", "Start a tunnel to the port, to share it (ngrok, cloudflared, tailscale funnel...)", "ports"},
	{"T", "Show the port's tunnel in the Tunnels tab", "ports"},
	{"x", "Stop the server (asks first; a container is stopped with docker stop)", "ports"},
	{"K", "Force stop the server (SIGKILL; asks first)", "ports"},
	{"Enter", "Open the tunnel's public URL in the browser", "tunnels"},
	{"o", "Open the local server the tunnel points at", "tunnels"},
	{"y", "Copy the public URL", "tunnels"},
	{"J / K", "Move through the tunnel's last requests (ngrok)", "tunnels"},
	{"R", "Send the selected request through the tunnel again (ngrok replay)", "tunnels"},
	{"i", "Open ngrok's request inspector in the browser", "tunnels"},
	{"x", "Stop the tunnel (asks first)", "tunnels"},
}

// KeybindingsFile is scripts/keybindings, for Vayu's keybinding search.
func KeybindingsFile() string {
	var b strings.Builder
	b.WriteString("# Keys inside the pattan screen, for Vayu's keybinding search\n")
	b.WriteString("# (keybind-search.py reads ~/.tmux/plugins/*/scripts). Generated from\n")
	b.WriteString("# internal/tui/keys.go by `go test ./internal/tui -update`; don't edit.\n")
	// marker is kept apart from the rest of the line: the binary holds this
	// format string, and the keybinding search scans plugins' bin/ folders.
	marker := strings.Join([]string{"#", "keybind"}, " ")
	for _, k := range Keys {
		where := ""
		if k.Where == "tunnels" {
			where = " (Tunnels tab)"
		}
		fmt.Fprintf(&b, "%s: Tmux/pattan | %s | %s%s\n", marker, k.Keys, k.Desc, where)
	}
	return b.String()
}
