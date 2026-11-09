package tui

import (
	"github.com/Theyashsawarkar/vyakarana/keymap"
	"github.com/Theyashsawarkar/vyakarana/mode"
)

// Where a key works: on a port's row or a tunnel's.
const (
	onPort   = "ports"
	onTunnel = "tunnels"
)

// Keys is every key of the screen, in the order the help sheet lists
// them, with the vim grammar every Vayu tool shares (vyakarana): NORMAL
// keys, a Space leader, INSERT while filtering. The README's key tables
// and scripts/keybindings (Vayu's keybinding search) come from it: after a
// change run `go test ./internal/tui -update`.
var Keys = keymap.New("pattan",
	keymap.Common{Noun: "port", Tabs: true,
		Search:  "Search the list by port, app, project or URL (filters it)",
		Refresh: "Rescan the ports now (it also rescans every 2 s)"}.Binds(),
	[]keymap.Binding{
		{Seq: "l", Alt: []string{"Right"}, Act: keymap.ActNextTab, Desc: "Switch to the next tab", Hide: true},
		{Seq: "h", Alt: []string{"Left"}, Act: keymap.ActPrevTab, Desc: "Switch to the previous tab", Hide: true},
		{Seq: "Ctrl+c", Act: keymap.ActQuit, Desc: "Quit", Hide: true},
		{Seq: "Space t d", Alt: []string{"Space t 1"}, Act: "tab-dev", Desc: "Switch to Dev servers"},
		{Seq: "Space t a", Alt: []string{"Space t 2"}, Act: "tab-all", Desc: "Switch to All listening (every port)"},
		{Seq: "Space t t", Alt: []string{"Space t 3"}, Act: "tab-tunnels", Desc: "Switch to Tunnels"},
		{Seq: "Space t k", Alt: []string{"Space t 4"}, Act: "tab-docker", Desc: "Switch to Docker"},
		{Seq: "a", Moved: "Space t a"},

		{Seq: "Enter", Act: "open", Desc: "Open localhost:port in the browser", Where: onPort},
		{Seq: "o", Alt: []string{"Space o b"}, Act: "open", Desc: "Open localhost:port in the browser", Where: onPort},
		{Seq: "y", Alt: []string{"Space y u"}, Act: "copy-url", Desc: "Copy the URL", Where: onPort},
		{Seq: "Y", Alt: []string{"Space y l"}, Act: "copy-lan", Desc: "Copy the URL other machines on your network can use", Where: onPort},
		{Seq: "Space y p", Act: "copy-public", Desc: "Copy the public URL of the port's tunnel", Where: onPort},
		{Seq: "d d", Alt: []string{"x", "Space d s"}, Act: "stop", Desc: "Stop the server (asks first; a container is stopped with docker stop)", Where: onPort},
		{Seq: "Space d k", Act: "kill", Desc: "Kill the server at once (SIGKILL; asks first)", Where: onPort},
		{Seq: "Space d t", Act: "stop-tunnel", Desc: "Stop the port's tunnel (asks first)", Where: onPort},
		{Seq: "Space a t", Act: "share", Desc: "Start a tunnel to the port, to share it (ngrok, cloudflared, tailscale funnel...)", Where: onPort},
		{Seq: "Space g p", Act: "go-pane", Desc: "Go to the tmux pane the server runs in", Where: onPort},
		{Seq: "Space g t", Act: "go-tunnel", Desc: "Go to the port's tunnel in the Tunnels tab", Where: onPort},
		{Seq: "t", Moved: "Space a t", Where: onPort},
		{Seq: "T", Moved: "Space g t", Where: onPort},
		{Seq: "K", Moved: "Space d k", Where: onPort},

		{Seq: "Enter", Alt: []string{"Space o b"}, Act: "open", Desc: "Open the tunnel's public URL in the browser", Where: onTunnel},
		{Seq: "o", Alt: []string{"Space o l"}, Act: "open-local", Desc: "Open the local server the tunnel points at", Where: onTunnel},
		{Seq: "y", Alt: []string{"Space y p"}, Act: "copy-public", Desc: "Copy the public URL", Where: onTunnel},
		{Seq: "Space y u", Act: "copy-local", Desc: "Copy the local URL the tunnel points at", Where: onTunnel},
		{Seq: "J", Act: "req-next", Desc: "Move to the next of the tunnel's last requests (ngrok)", Where: onTunnel},
		{Seq: "K", Act: "req-prev", Desc: "Move to the previous of the tunnel's last requests (ngrok)", Where: onTunnel},
		{Seq: "Space i r", Act: "replay", Desc: "Send the selected request through the tunnel again (ngrok replay)", Where: onTunnel},
		{Seq: "Space i i", Act: "inspect", Desc: "Open ngrok's request inspector in the browser", Where: onTunnel},
		{Seq: "d d", Alt: []string{"x", "Space d t"}, Act: "stop-tunnel", Desc: "Stop the tunnel (asks first)", Where: onTunnel},
		{Seq: "R", Moved: "Space i r", Where: onTunnel},
		{Seq: "i", Moved: "Space i i", Where: onTunnel},

		{Seq: "Esc", Act: "insert-done", Desc: "Stop typing and keep the filter (back to NORMAL)", Modes: []mode.Mode{mode.Insert}},
		{Seq: "Enter", Act: "insert-done", Desc: "Stop typing and keep the filter", Modes: []mode.Mode{mode.Insert}, Hide: true},
		{Seq: "Ctrl+j", Alt: []string{"Down", "Ctrl+n"}, Act: keymap.ActDown, Desc: "Move to the next port while typing", Modes: []mode.Mode{mode.Insert}},
		{Seq: "Ctrl+k", Alt: []string{"Up", "Ctrl+p"}, Act: keymap.ActUp, Desc: "Move to the previous port while typing", Modes: []mode.Mode{mode.Insert}},
	}).
	Group("Space i", "Inspect").
	Where(onPort, "On a port").
	Where(onTunnel, "On a tunnel")
