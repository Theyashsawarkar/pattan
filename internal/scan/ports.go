// Package scan finds what listens on this machine's TCP ports: the
// process, the app it is (vite, django...), its project folder, Docker's
// published ports, whether it answers HTTP, and the tunnels that make a
// port public.
package scan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Listener is one port something listens on.
type Listener struct {
	Port    int
	Addrs   []string // what it is bound to: "127.0.0.1", "*", "[::1]"...
	PID     int      // 0 when the process isn't ours to see (root, another user)
	Proc    string   // the process name ss reports
	Cmdline []string
	Cwd     string
	Started time.Time
	RSS     int64 // bytes
	CPU     float64
	Conns   int // established connections to it

	App, Kind string // "vite", "node"
	Project   string // where it runs, shortened: "onads-super/apps/web"
	Dev       bool   // one of your dev servers (yours, started inside a project folder)
	Tunnel    bool   // a tunnel tool's own port (ngrok's inspector...)
	Docker    *Container
}

// Public says whether other machines can reach it (not only loopback).
func (l Listener) Public() bool {
	for _, a := range l.Addrs {
		if !strings.HasPrefix(a, "127.") && a != "[::1]" && a != "localhost" {
			return true
		}
	}
	return false
}

// URL is how to open it here.
func (l Listener) URL() string { return "http://localhost:" + strconv.Itoa(l.Port) }

// Container is a Docker container publishing a port.
type Container struct {
	ID, Name, Image, Status string
}

var reUsers = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// ignore are your own processes that listen but aren't servers you'd
// open.
var ignore = map[string]bool{"adb": true, "kdeconnectd": true, "mpd": true, "syncthing": true}

var home, _ = os.UserHomeDir()

// Scanner keeps what it needs between scans (CPU times).
type Scanner struct {
	prevCPU map[int]uint64
	prevAt  time.Time
}

// Listening lists every TCP port something listens on, merged across
// IPv4 and IPv6, sorted by port.
func (s *Scanner) Listening(ctx context.Context) ([]Listener, error) {
	out, err := run(ctx, "ss", "-ltnpH")
	if err != nil {
		return nil, err
	}
	byPort := map[int]*Listener{}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Fields(line)
		if len(cols) < 5 {
			continue
		}
		local := cols[3]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		addr := local[:i]
		if j := strings.Index(addr, "%"); j >= 0 {
			addr = addr[:j]
		}
		if addr == "0.0.0.0" || addr == "[::]" {
			addr = "*"
		}
		l := byPort[port]
		if l == nil {
			l = &Listener{Port: port}
			byPort[port] = l
		}
		if !contains(l.Addrs, addr) {
			l.Addrs = append(l.Addrs, addr)
		}
		if m := reUsers.FindStringSubmatch(line); m != nil && l.PID == 0 {
			l.Proc = m[1]
			l.PID, _ = strconv.Atoi(m[2])
		}
	}
	conns := established(ctx)
	now := time.Now()
	cpu := map[int]uint64{}
	var ls []Listener
	for _, l := range byPort {
		l.Conns = conns[l.Port]
		if l.PID > 0 {
			s.fill(l, now, cpu)
		}
		ls = append(ls, *l)
	}
	s.prevCPU, s.prevAt = cpu, now
	sort.Slice(ls, func(i, j int) bool { return ls[i].Port < ls[j].Port })
	return ls, nil
}

// fill reads the process's details from /proc.
func (s *Scanner) fill(l *Listener, now time.Time, cpu map[int]uint64) {
	pid := strconv.Itoa(l.PID)
	if b, err := os.ReadFile("/proc/" + pid + "/cmdline"); err == nil {
		for _, a := range bytes.Split(bytes.TrimRight(b, "\x00"), []byte{0}) {
			l.Cmdline = append(l.Cmdline, string(a))
		}
	}
	l.Cwd, _ = os.Readlink("/proc/" + pid + "/cwd")
	if st, err := readStat(l.PID); err == nil {
		l.Started = bootTime().Add(time.Duration(st.start) * time.Second / clockTicks)
		cpu[l.PID] = st.cpu
		if prev, ok := s.prevCPU[l.PID]; ok && !s.prevAt.IsZero() {
			secs := now.Sub(s.prevAt).Seconds()
			if secs > 0 && st.cpu >= prev {
				l.CPU = float64(st.cpu-prev) / clockTicks / secs * 100
			}
		}
	}
	if b, err := os.ReadFile("/proc/" + pid + "/statm"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 1 {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			l.RSS = n * int64(os.Getpagesize())
		}
	}
	l.App, l.Kind = Identify(l.Proc, l.Cmdline, l.Cwd)
	l.Project = Project(l.Cwd)
	l.Tunnel = tunnelTool(l.Proc, l.Cmdline) != ""
	inProject := l.Cwd != "" && strings.HasPrefix(l.Cwd, home+"/") && !strings.Contains(l.Cwd[len(home):], "/.")
	l.Dev = l.Port >= 1024 && !ignore[l.Proc] && inProject && !l.Tunnel
}

// established counts open connections per local port.
func established(ctx context.Context) map[int]int {
	out, err := run(ctx, "ss", "-tnH", "state", "established")
	n := map[int]int{}
	if err != nil {
		return n
	}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Fields(line)
		if len(cols) < 4 {
			continue
		}
		local := cols[2]
		if i := strings.LastIndex(local, ":"); i >= 0 {
			if p, err := strconv.Atoi(local[i+1:]); err == nil {
				n[p]++
			}
		}
	}
	return n
}

// Kinds of server: the app's kind decides its icon and colour.
var kindRules = []struct {
	rx   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`^(node|npm|npx|pnpm|yarn|bun|deno|next-server|vite|nuxt|astro|tsx|ts-node)`), "node"},
	{regexp.MustCompile(`^(python|uvicorn|gunicorn|flask|django|hypercorn|daphne|jupyter|streamlit)`), "python"},
	{regexp.MustCompile(`^(ruby|puma|rails|unicorn)`), "ruby"},
	{regexp.MustCompile(`^(java|gradle|mvn)`), "java"},
	{regexp.MustCompile(`^(php|php-fpm|artisan|frankenphp)`), "php"},
	{regexp.MustCompile(`^(go|air)$`), "go"},
	{regexp.MustCompile(`^(cargo|rustc)`), "rust"},
	{regexp.MustCompile(`^dotnet`), "dotnet"},
	{regexp.MustCompile(`^docker-proxy`), "docker"},
	{regexp.MustCompile(`^(postgres|postmaster)`), "postgres"},
	{regexp.MustCompile(`^(redis)`), "redis"},
	{regexp.MustCompile(`^(mysqld|mariadbd)`), "mysql"},
	{regexp.MustCompile(`^(mongod)`), "mongo"},
	{regexp.MustCompile(`^(nginx|caddy|httpd|hugo|jekyll)`), "web"},
	{regexp.MustCompile(`^(ngrok|cloudflared|bore|zrok|tailscaled)`), "tunnel"},
}

// What a command line runs, checked in order: the first match names it.
var appRules = []struct {
	rx  *regexp.Regexp
	app string
}{
	{regexp.MustCompile(`(^|/)vite(\.js)?( |$)|/vite/bin/`), "vite"},
	{regexp.MustCompile(`next-server|(^|/)next( |$)|/next/dist/`), "next"},
	{regexp.MustCompile(`(^|/)nest( |$)|@nestjs/cli|/dist/main(\.js)?$`), "nest"},
	{regexp.MustCompile(`(^|/)astro( |$)|/astro/`), "astro"},
	{regexp.MustCompile(`(^|/)nuxi?( |$)|/nuxt/`), "nuxt"},
	{regexp.MustCompile(`remix|react-router dev`), "remix"},
	{regexp.MustCompile(`svelte-kit|sveltekit`), "sveltekit"},
	{regexp.MustCompile(`react-scripts`), "create-react-app"},
	{regexp.MustCompile(`(^|/)ng (serve|s)( |$)|@angular/cli`), "angular"},
	{regexp.MustCompile(`storybook`), "storybook"},
	{regexp.MustCompile(`gatsby`), "gatsby"},
	{regexp.MustCompile(`expo|metro`), "expo"},
	{regexp.MustCompile(`webpack`), "webpack"},
	{regexp.MustCompile(`parcel`), "parcel"},
	{regexp.MustCompile(`docusaurus`), "docusaurus"},
	{regexp.MustCompile(`vitepress`), "vitepress"},
	{regexp.MustCompile(`json-server`), "json-server"},
	{regexp.MustCompile(`manage\.py runserver`), "django"},
	{regexp.MustCompile(`uvicorn`), "uvicorn"},
	{regexp.MustCompile(`gunicorn`), "gunicorn"},
	{regexp.MustCompile(`flask`), "flask"},
	{regexp.MustCompile(`fastapi`), "fastapi"},
	{regexp.MustCompile(`streamlit`), "streamlit"},
	{regexp.MustCompile(`jupyter`), "jupyter"},
	{regexp.MustCompile(`http\.server`), "http.server"},
	{regexp.MustCompile(`mkdocs`), "mkdocs"},
	{regexp.MustCompile(`rails|puma`), "rails"},
	{regexp.MustCompile(`artisan serve`), "laravel"},
	{regexp.MustCompile(`php .*-S `), "php -S"},
	{regexp.MustCompile(`(^|/)hugo( |$)`), "hugo"},
	{regexp.MustCompile(`jekyll`), "jekyll"},
	{regexp.MustCompile(`(^|/)air( |$)`), "air"},
	{regexp.MustCompile(`spring-boot|bootRun`), "spring"},
	{regexp.MustCompile(`live-server|browser-sync|http-server|serve -s`), "static server"},
}

// Identify names the app a process runs and its kind: from its command
// line, else the framework its project's package.json uses, else the
// script it runs.
func Identify(proc string, cmdline []string, cwd string) (app, kind string) {
	kind = "other"
	for _, r := range kindRules {
		if r.rx.MatchString(proc) {
			kind = r.kind
			break
		}
	}
	line := strings.Join(cmdline, " ")
	for _, r := range appRules {
		if r.rx.MatchString(line) {
			return r.app, kind
		}
	}
	fw, strong := "", false
	if kind == "node" {
		fw, strong = framework(cwd)
		if strong {
			return fw, kind
		}
	}
	if kind == "node" || kind == "python" {
		if s := script(cmdline); s != "" {
			return s, kind
		}
	}
	if fw != "" { // a server library: express, ws...
		return fw, kind
	}
	return proc, kind
}

// script is the script a node or python command runs, without folders
// and extension: "bin/ekatva.js daemon" is "ekatva".
func script(cmdline []string) string {
	takesValue := map[string]bool{"--require": true, "-r": true, "--import": true, "--loader": true, "--experimental-loader": true, "-m": false}
	for i := 1; i < len(cmdline); i++ {
		a := cmdline[i]
		if strings.HasPrefix(a, "-") {
			if takesValue[a] {
				i++
			}
			continue
		}
		if strings.Contains(a, "node_modules/") || strings.Contains(a, "site-packages/") {
			continue
		}
		b := filepath.Base(a)
		for _, ext := range []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".py"} {
			b = strings.TrimSuffix(b, ext)
		}
		switch b {
		case "", "index", "main", "server", "app", "watch", "dev", "start", "run":
			return ""
		}
		return b
	}
	return ""
}

// frameworks a package.json can depend on, the most specific first.
// Frameworks name the app outright; libraries only when the script's
// name says nothing.
var frameworks = []struct {
	dep, name string
	strong    bool
}{
	{"@nestjs/core", "nest", true}, {"next", "next", true}, {"nuxt", "nuxt", true}, {"astro", "astro", true},
	{"@sveltejs/kit", "sveltekit", true}, {"@remix-run/dev", "remix", true}, {"vite", "vite", true},
	{"hono", "hono", false}, {"fastify", "fastify", false}, {"koa", "koa", false}, {"express", "express", false},
	{"@hapi/hapi", "hapi", false}, {"socket.io", "socket.io", false}, {"ws", "ws", false},
}

// framework reads the nearest package.json from dir up to home.
func framework(dir string) (string, bool) {
	for d := dir; d != "" && d != "/" && strings.HasPrefix(d, home); d = filepath.Dir(d) {
		b, err := os.ReadFile(filepath.Join(d, "package.json"))
		if err != nil {
			continue
		}
		var pkg struct {
			Dependencies, DevDependencies map[string]string
		}
		if json.Unmarshal(b, &pkg) != nil {
			return "", false
		}
		for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
			for _, f := range frameworks {
				if _, ok := deps[f.dep]; ok {
					return f.name, f.strong
				}
			}
		}
		return "", false
	}
	return "", false
}

// containers that hold projects; a path under one is shown without it.
var codeDirs = map[string]bool{"code": true, "codes": true, "Code": true, "projects": true, "Projects": true, "personal": true,
	"src": true, "dev": true, "work": true, "repos": true, "git": true, "workspace": true, "Documents": true}

// Project shortens a working directory: home and a code folder dropped,
// "~/codes/onads-super/apps/web" is "onads-super/apps/web".
func Project(cwd string) string {
	if cwd == "" {
		return ""
	}
	if cwd == home {
		return "~"
	}
	if !strings.HasPrefix(cwd, home+"/") {
		return cwd
	}
	rel := cwd[len(home)+1:]
	if first, rest, ok := strings.Cut(rel, "/"); ok && codeDirs[first] {
		return rest
	}
	return "~/" + rel
}

// Containers lists running Docker containers and the host ports each
// publishes. No Docker, or no access to it, is no containers.
func Containers(ctx context.Context) map[int]*Container {
	byPort := map[int]*Container{}
	if _, err := exec.LookPath("docker"); err != nil {
		return byPort
	}
	out, err := run(ctx, "docker", "ps", "--format", "{{json .}}")
	if err != nil {
		return byPort
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var c struct{ ID, Names, Image, Status, Ports string }
		if json.Unmarshal(sc.Bytes(), &c) != nil {
			continue
		}
		ct := &Container{ID: c.ID, Name: c.Names, Image: c.Image, Status: c.Status}
		for _, p := range strings.Split(c.Ports, ",") {
			host, _, ok := strings.Cut(strings.TrimSpace(p), "->")
			if !ok {
				continue
			}
			if i := strings.LastIndex(host, ":"); i >= 0 {
				if n, err := strconv.Atoi(host[i+1:]); err == nil {
					byPort[n] = ct
				}
			}
		}
	}
	return byPort
}

// Pane is the tmux pane a process runs in.
type Pane struct {
	ID, Target, Name string // "%12", "main:2.1", the window's name
}

// Panes maps each listener's PID to the tmux pane it runs in (any parent
// process being a pane's shell).
func Panes(ctx context.Context, pids []int) map[int]Pane {
	found := map[int]Pane{}
	if os.Getenv("TMUX") == "" {
		return found
	}
	out, err := run(ctx, "tmux", "list-panes", "-a", "-F", "#{pane_pid}\t#{pane_id}\t#{session_name}:#{window_index}.#{pane_index}\t#{window_name}")
	if err != nil {
		return found
	}
	byPid := map[int]Pane{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		byPid[pid] = Pane{ID: f[1], Target: f[2], Name: f[3]}
	}
	for _, pid := range pids {
		for p, hops := pid, 0; p > 1 && hops < 40; hops++ {
			if pane, ok := byPid[p]; ok {
				found[pid] = pane
				break
			}
			st, err := readStat(p)
			if err != nil {
				break
			}
			p = st.ppid
		}
	}
	return found
}

// ------------------------------------------------------------ /proc

const clockTicks = 100 // USER_HZ, 100 on every Linux we run on

type procStat struct {
	ppid  int
	cpu   uint64 // utime+stime, in ticks
	start uint64 // ticks after boot
}

func readStat(pid int) (procStat, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, err
	}
	// the name is in parentheses and may hold spaces: fields start after ")"
	i := bytes.LastIndexByte(b, ')')
	f := strings.Fields(string(b[i+2:]))
	if len(f) < 20 {
		return procStat{}, os.ErrInvalid
	}
	var st procStat
	st.ppid, _ = strconv.Atoi(f[1])
	ut, _ := strconv.ParseUint(f[11], 10, 64)
	stt, _ := strconv.ParseUint(f[12], 10, 64)
	st.cpu = ut + stt
	st.start, _ = strconv.ParseUint(f[19], 10, 64)
	return st, nil
}

var boot time.Time

func bootTime() time.Time {
	if !boot.IsZero() {
		return boot
	}
	b, _ := os.ReadFile("/proc/stat")
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			boot = time.Unix(n, 0)
		}
	}
	return boot
}

// Procs lists your own processes: pid → command line.
func Procs() map[int][]string {
	out := map[int][]string{}
	ents, _ := os.ReadDir("/proc")
	uid := os.Getuid()
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if info, err := e.Info(); err == nil {
			if st, ok := statUID(info); ok && st != uid {
				continue
			}
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(b) == 0 {
			continue
		}
		var args []string
		for _, a := range bytes.Split(bytes.TrimRight(b, "\x00"), []byte{0}) {
			args = append(args, string(a))
		}
		out[pid] = args
	}
	return out
}

// Started is when a process started.
func Started(pid int) time.Time {
	st, err := readStat(pid)
	if err != nil {
		return time.Time{}
	}
	return bootTime().Add(time.Duration(st.start) * time.Second / clockTicks)
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
