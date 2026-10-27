package scan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Tunnel makes a local port reachable from outside: ngrok, cloudflared,
// localtunnel, tailscale serve/funnel, ssh -R, bore, zrok.
type Tunnel struct {
	Tool    string // "ngrok", "cloudflared"...
	Public  string // the public URL or host, "" when the tool doesn't say
	Port    int    // the local port it forwards to (0: unknown)
	Local   string // what it forwards to, as given: "http://localhost:5173"
	PID     int
	Started time.Time
	Reqs    int    // requests (ngrok) or connections so far, -1: unknown
	API     string // ngrok's local API, "http://127.0.0.1:4040"
	Name    string // ngrok's tunnel name, for its request list
	Note    string // "tailnet only", "named tunnel onads"...
	Cmd     string
}

// Seen says whether it was found with a public address.
func (t Tunnel) Seen() bool { return t.Public != "" }

func tunnelTool(proc string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	base := filepath.Base(args[0])
	line := strings.Join(args, " ")
	switch {
	case base == "ngrok" || proc == "ngrok":
		return "ngrok"
	case base == "cloudflared" || proc == "cloudflared":
		if strings.Contains(line, " tunnel") {
			return "cloudflared"
		}
	case base == "bore" && len(args) > 1 && args[1] == "local":
		return "bore"
	case base == "zrok" && strings.Contains(line, " share"):
		return "zrok"
	case base == "lt" || strings.HasSuffix(line, "/localtunnel/bin/lt.js") || strings.Contains(line, "localtunnel/bin/lt"):
		return "localtunnel"
	case base == "ssh" && sshRemote(args) != nil:
		return "ssh -R"
	}
	return ""
}

// Tunnels finds the tunnels running now. ls are this scan's listeners
// (a tunnel tool's own ports lead to its API).
func Tunnels(ctx context.Context, ls []Listener, procs map[int][]string, tailscale bool) []Tunnel {
	ports := map[int][]int{} // pid → ports it listens on
	for _, l := range ls {
		if l.PID > 0 {
			ports[l.PID] = append(ports[l.PID], l.Port)
		}
	}
	var out []Tunnel
	for pid, args := range procs {
		tool := tunnelTool("", args)
		if tool == "" {
			continue
		}
		base := Tunnel{Tool: tool, PID: pid, Started: Started(pid), Reqs: -1, Cmd: strings.Join(args, " ")}
		switch tool {
		case "ngrok":
			out = append(out, ngrok(ctx, base, ports[pid])...)
		case "cloudflared":
			out = append(out, cloudflared(ctx, base, args, ports[pid]))
		case "localtunnel":
			base.Port = flagInt(args, "--port", "-p")
			base.Local = localURL(base.Port)
			if sub := flagStr(args, "--subdomain", "-s"); sub != "" {
				base.Public = "https://" + sub + ".loca.lt"
			} else {
				base.Note = "its URL is in its own output"
			}
			out = append(out, base)
		case "bore":
			if len(args) > 2 {
				base.Port, _ = strconv.Atoi(args[2])
			}
			base.Local = localURL(base.Port)
			host := flagStr(args, "--to", "-t")
			if host == "" {
				host = "bore.pub"
			}
			if rp := flagStr(args, "--port", "-p"); rp != "" {
				base.Public = host + ":" + rp
			} else {
				base.Public, base.Note = host, "port picked by the server: in its output"
			}
			out = append(out, base)
		case "zrok":
			for _, a := range args {
				if strings.Contains(a, "localhost:") || strings.Contains(a, "127.0.0.1:") {
					base.Local = a
					base.Port = portOf(a)
				}
			}
			base.Note = "its URL is in its own output"
			out = append(out, base)
		case "ssh -R":
			dest := sshDest(args)
			for _, r := range sshRemote(args) {
				t := base
				t.Port, t.Local = r.localPort, localURL(r.localPort)
				t.Public = dest + ":" + strconv.Itoa(r.remotePort)
				t.Note = "on " + dest
				out = append(out, t)
			}
		}
	}
	if tailscale {
		out = append(out, tailscaleServe(ctx)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seen() != out[j].Seen() {
			return out[i].Seen()
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Public < out[j].Public
	})
	return out
}

// ngrok: its local API (4040, or the next free port) lists its tunnels.
func ngrok(ctx context.Context, base Tunnel, ports []int) []Tunnel {
	try := ports
	if len(try) == 0 {
		try = []int{4040, 4041, 4042}
	}
	for _, p := range try {
		api := "http://127.0.0.1:" + strconv.Itoa(p)
		var data struct {
			Tunnels []struct {
				Name      string
				PublicURL string `json:"public_url"`
				Config    struct{ Addr string }
				Metrics   struct {
					Conns struct{ Count int }
					HTTP  struct{ Count int } `json:"http"`
				}
			}
		}
		if getJSON(ctx, api+"/api/tunnels", &data) != nil {
			continue
		}
		var out []Tunnel
		for _, t := range data.Tunnels {
			tt := base
			tt.API, tt.Name, tt.Public = api, t.Name, t.PublicURL
			tt.Local, tt.Port = t.Config.Addr, portOf(t.Config.Addr)
			tt.Reqs = max(t.Metrics.HTTP.Count, t.Metrics.Conns.Count)
			out = append(out, tt)
		}
		if len(out) > 0 {
			return out
		}
	}
	base.Note = "starting, or its API is off"
	return []Tunnel{base}
}

// cloudflared: a quick tunnel says its hostname on its metrics server.
func cloudflared(ctx context.Context, base Tunnel, args []string, ports []int) Tunnel {
	base.Local = flagStr(args, "--url")
	base.Port = portOf(base.Local)
	for _, p := range ports {
		var q struct{ Hostname string }
		if getJSON(ctx, "http://127.0.0.1:"+strconv.Itoa(p)+"/quicktunnel", &q) == nil && q.Hostname != "" {
			base.Public = "https://" + q.Hostname
			return base
		}
	}
	if name := lastArg(args); name != "" && !strings.HasPrefix(name, "-") && name != "tunnel" && name != "run" && base.Local == "" {
		base.Note = "named tunnel " + name
	} else {
		base.Note = "its URL is in its own output"
	}
	return base
}

// tailscale serve/funnel: what this machine serves on the tailnet, and
// which of it is public (funnel).
func tailscaleServe(ctx context.Context) []Tunnel {
	out, err := run(ctx, "tailscale", "serve", "status", "--json")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var st struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
		AllowFunnel map[string]bool
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		return nil
	}
	var ts []Tunnel
	for hostport, w := range st.Web {
		host := strings.TrimSuffix(hostport, ":443")
		for path, h := range w.Handlers {
			t := Tunnel{Tool: "tailscale", Public: "https://" + host + strings.TrimSuffix(path, "/"), Local: h.Proxy, Port: portOf(h.Proxy), Reqs: -1}
			if st.AllowFunnel[hostport] {
				t.Note = "funnel: public"
			} else {
				t.Note = "serve: tailnet only"
			}
			ts = append(ts, t)
		}
	}
	return ts
}

type sshFwd struct{ remotePort, localPort int }

// sshRemote reads ssh's -R forwards: [bind:]port:host:hostport.
func sshRemote(args []string) []sshFwd {
	var out []sshFwd
	for i := 1; i < len(args); i++ {
		spec := ""
		switch {
		case args[i] == "-R" && i+1 < len(args):
			spec = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-R"):
			spec = args[i][2:]
		default:
			continue
		}
		f := strings.Split(spec, ":")
		if len(f) < 3 {
			continue
		}
		rp, err1 := strconv.Atoi(f[len(f)-3])
		lp, err2 := strconv.Atoi(f[len(f)-1])
		if err1 == nil && err2 == nil {
			out = append(out, sshFwd{rp, lp})
		}
	}
	return out
}

// sshDest is the host ssh connects to: the first argument that isn't an
// option or an option's value.
func sshDest(args []string) string {
	withValue := "BbcDEeFIiJLlmOoPpQRSWw"
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if len(a) == 2 && strings.ContainsRune(withValue, rune(a[1])) {
				i++
			}
			continue
		}
		if _, h, ok := strings.Cut(a, "@"); ok {
			return h
		}
		return a
	}
	return "?"
}

// Requests are ngrok's last requests through a tunnel.
type Request struct {
	ID       string
	Method   string
	URI      string
	Status   int
	Duration time.Duration
	At       time.Time
}

// NgrokRequests lists a tunnel's last requests, newest first.
func NgrokRequests(ctx context.Context, t Tunnel) ([]Request, error) {
	if t.API == "" {
		return nil, fmt.Errorf("no inspector")
	}
	var data struct {
		Requests []struct {
			ID       string
			Start    time.Time
			Duration int64
			Request  struct{ Method, URI string }
			Response struct {
				StatusCode int `json:"status_code"`
			}
		}
	}
	u := t.API + "/api/requests/http?limit=30&tunnel_name=" + url.QueryEscape(t.Name)
	if err := getJSON(ctx, u, &data); err != nil {
		return nil, err
	}
	var out []Request
	for _, r := range data.Requests {
		out = append(out, Request{ID: r.ID, Method: r.Request.Method, URI: r.Request.URI, Status: r.Response.StatusCode,
			Duration: time.Duration(r.Duration), At: r.Start})
	}
	return out, nil
}

// Replay sends a captured request through the tunnel again.
func Replay(ctx context.Context, t Tunnel, id string) error {
	body, _ := json.Marshal(map[string]string{"id": id, "tunnel_name": t.Name})
	req, _ := http.NewRequestWithContext(ctx, "POST", t.API+"/api/requests/http", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ngrok said %s", resp.Status)
	}
	return nil
}

// Sharer is a tunnel tool that's installed, and how it shares a port.
type Sharer struct {
	Tool, Note string
	Args       func(port int) []string
}

// Sharers are the tunnel tools on PATH.
func Sharers() []Sharer {
	all := []Sharer{
		{"ngrok", "needs your ngrok authtoken", func(p int) []string { return []string{"ngrok", "http", strconv.Itoa(p), "--log", "stdout"} }},
		{"cloudflared", "no account", func(p int) []string {
			return []string{"cloudflared", "tunnel", "--url", "http://localhost:" + strconv.Itoa(p)}
		}},
		{"tailscale", "funnel: public on your tailnet name", func(p int) []string { return []string{"tailscale", "funnel", "--bg", strconv.Itoa(p)} }},
		{"lt", "localtunnel", func(p int) []string { return []string{"lt", "--port", strconv.Itoa(p)} }},
		{"bore", "bore.pub", func(p int) []string { return []string{"bore", "local", strconv.Itoa(p), "--to", "bore.pub"} }},
	}
	var out []Sharer
	for _, s := range all {
		if _, err := exec.LookPath(s.Tool); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// Share starts a tunnel to port in the background, on its own (it keeps
// running after pattan quits), its output in a log file. It waits a
// moment and reports the tool's last words if it stopped right away.
func Share(s Sharer, port int) (logFile string, err error) {
	dir := filepath.Join(stateDir(), "pattan")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	logFile = filepath.Join(dir, fmt.Sprintf("%s-%d.log", s.Tool, port))
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	args := s.Args(port)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return logFile, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		if s.Tool == "tailscale" { // --bg configures it and exits
			if b, _ := os.ReadFile(logFile); bytes.Contains(b, []byte("rror")) || bytes.Contains(b, []byte("denied")) {
				return logFile, fmt.Errorf("%s", lastLine(b))
			}
			return logFile, nil
		}
		b, _ := os.ReadFile(logFile)
		return logFile, fmt.Errorf("%s stopped: %s", s.Tool, lastLine(b))
	case <-time.After(2500 * time.Millisecond):
		return logFile, nil
	}
}

// StopTunnel ends a tunnel: its process, or tailscale's serve config.
func StopTunnel(t Tunnel) error {
	if t.Tool == "tailscale" {
		pub := strings.TrimPrefix(t.Public, "https://")
		_, path, _ := strings.Cut(pub, "/")
		args := []string{"funnel", "--https=443"}
		if path != "" {
			args = append(args, "--set-path=/"+path)
		}
		args = append(args, "off")
		out, err := exec.Command("tailscale", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", lastLine(out))
		}
		return nil
	}
	if t.PID <= 0 {
		return fmt.Errorf("no process to stop")
	}
	return syscall.Kill(t.PID, syscall.SIGTERM)
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d
	}
	return filepath.Join(home, ".local", "state")
}

func getJSON(ctx context.Context, u string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

var rePort = regexp.MustCompile(`:(\d{2,5})(/|$)`)

func portOf(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if m := rePort.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if strings.HasPrefix(s, "http://") {
		return 80
	}
	return 0
}

func localURL(port int) string {
	if port == 0 {
		return ""
	}
	return "http://localhost:" + strconv.Itoa(port)
}

func flagStr(args []string, names ...string) string {
	for i, a := range args {
		for _, n := range names {
			if a == n && i+1 < len(args) {
				return args[i+1]
			}
			if v, ok := strings.CutPrefix(a, n+"="); ok {
				return v
			}
		}
	}
	return ""
}

func flagInt(args []string, names ...string) int {
	n, _ := strconv.Atoi(flagStr(args, names...))
	return n
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if l == "" {
		return "no output"
	}
	if len(l) > 160 {
		l = l[:160] + "…"
	}
	return l
}
