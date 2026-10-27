// Package tui is pattan's screen: the ports in a list on the left, the
// selected one's details on the right.
package tui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Theyashsawarkar/pattan/internal/scan"
	"github.com/Theyashsawarkar/pattan/internal/ui"
)

const (
	tabDev = iota
	tabAll
	tabTunnels
	tabDocker
)

var tabNames = []string{"Dev servers", "All listening", "Tunnels", "Docker"}

// Options set how the screen runs.
type Options struct {
	PopupKey string // in a tmux popup: this key closes it too
	Client   string // the tmux client to switch for g
}

// item is a row of the list: a listener, a tunnel, or a heading.
type item struct {
	l       *scan.Listener
	t       *scan.Tunnel
	heading string
}

type popup int

const (
	popNone popup = iota
	popHelp
	popConfirm
	popShare
)

type confirm struct {
	title, body, yes string
	color            string
	act              func() tea.Msg
}

// Model is the screen.
type Model struct {
	opt     Options
	st      ui.Styles
	stamp   string
	w, h    int
	tab     int
	sel     [4]int
	top     [4]int
	scanner *scan.Scanner
	scans   int
	loaded  bool
	err     string

	ls      []scan.Listener
	tunnels []scan.Tunnel
	panes   map[int]scan.Pane
	probes  map[int]scan.Probe
	probing map[int]bool
	reqs    map[string][]scan.Request
	reqSel  int
	ip      string

	filter    textinput.Model
	filtering bool

	pop     popup
	conf    *confirm
	share   []scan.Sharer
	shareP  int
	shareAt int
	helpTop int

	msg    string
	msgErr bool
	msgID  int
}

// New makes the screen.
func New(opt Options) Model {
	st := ui.New(ui.LoadTheme("pattan"))
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = "port, app, project…"
	return Model{opt: opt, st: st, stamp: ui.ThemeStamp(), scanner: &scan.Scanner{}, filter: f,
		panes: map[int]scan.Pane{}, probes: map[int]scan.Probe{}, probing: map[int]bool{}, reqs: map[string][]scan.Request{},
		ip: lanIP()}
}

// ------------------------------------------------------------- messages

type scanMsg struct {
	ls      []scan.Listener
	tunnels []scan.Tunnel
	panes   map[int]scan.Pane
	err     error
}
type tickMsg struct{}
type probeMsg struct {
	port int
	p    scan.Probe
}
type reqsMsg struct {
	key  string
	reqs []scan.Request
}
type doneMsg struct {
	text string
	err  error
	quit bool
}
type clearMsg struct{ id int }

func (m Model) Init() tea.Cmd { return m.scan() }

func (m Model) scan() tea.Cmd {
	sc := m.scanner
	withTS := m.scans%5 == 0
	return func() tea.Msg {
		ctx := context.Background()
		ls, err := sc.Listening(ctx)
		if err != nil {
			return scanMsg{err: fmt.Errorf("couldn't list ports (is ss, from iproute2, installed?): %w", err)}
		}
		docker := scan.Containers(ctx)
		seen := map[int]bool{}
		for i := range ls {
			if c, ok := docker[ls[i].Port]; ok {
				ls[i].Docker = c
				ls[i].App, ls[i].Kind = imageName(c.Image), "docker"
				ls[i].Project = c.Name
			}
			seen[ls[i].Port] = true
		}
		for p, c := range docker { // published but not seen by ss (rootless Docker)
			if !seen[p] {
				ls = append(ls, scan.Listener{Port: p, Addrs: []string{"*"}, Docker: c, App: imageName(c.Image), Kind: "docker", Project: c.Name})
			}
		}
		sort.Slice(ls, func(i, j int) bool { return ls[i].Port < ls[j].Port })
		_, hasTS := exec.LookPath("tailscale")
		tunnels := scan.Tunnels(ctx, ls, scan.Procs(), withTS && hasTS == nil)
		var pids []int
		for _, l := range ls {
			if l.PID > 0 {
				pids = append(pids, l.PID)
			}
		}
		return scanMsg{ls: ls, tunnels: tunnels, panes: scan.Panes(ctx, pids)}
	}
}

func imageName(img string) string {
	img = img[strings.LastIndex(img, "/")+1:]
	if i := strings.Index(img, ":"); i > 0 {
		img = img[:i]
	}
	return img
}

func tick() tea.Cmd { return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// ------------------------------------------------------------- update

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case scanMsg:
		m.scans++
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, tick()
		}
		m.err, m.loaded = "", true
		keepTS := m.scans%5 != 1
		if keepTS { // tailscale is asked every 5th scan: keep what it said
			for _, t := range m.tunnels {
				if t.Tool == "tailscale" {
					msg.tunnels = append(msg.tunnels, t)
				}
			}
		}
		m.ls, m.tunnels, m.panes = msg.ls, msg.tunnels, msg.panes
		m.clamp()
		return m, tea.Batch(tick(), m.probeCmds(), m.reqsCmd())
	case tickMsg:
		if s := ui.ThemeStamp(); s != m.stamp {
			m.stamp, m.st = s, ui.New(ui.LoadTheme("pattan"))
		}
		return m, m.scan()
	case probeMsg:
		m.probes[msg.port], m.probing[msg.port] = msg.p, false
		return m, nil
	case reqsMsg:
		m.reqs[msg.key] = msg.reqs
		return m, nil
	case doneMsg:
		if msg.quit {
			return m, tea.Quit
		}
		cmd := m.say(msg.text, msg.err)
		return m, tea.Batch(cmd, m.scan())
	case clearMsg:
		if msg.id == m.msgID {
			m.msg = ""
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) say(text string, err error) tea.Cmd {
	m.msgErr = err != nil
	if err != nil {
		text = err.Error()
	}
	if text == "" {
		return nil
	}
	m.msg = text
	m.msgID++
	id := m.msgID
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearMsg{id} })
}

// probeCmds asks the ports shown that haven't been asked lately.
func (m Model) probeCmds() tea.Cmd {
	var cmds []tea.Cmd
	cur := m.current()
	for i := range m.ls {
		l := m.ls[i]
		shown := l.Dev || l.Docker != nil || (cur.l != nil && cur.l.Port == l.Port)
		if !shown || m.probing[l.Port] {
			continue
		}
		if p, ok := m.probes[l.Port]; ok && time.Since(p.At) < 10*time.Second {
			continue
		}
		m.probing[l.Port] = true
		port, httpToo := l.Port, !scan.SkipHTTP(l)
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return probeMsg{port, scan.ProbePort(ctx, port, httpToo)}
		})
	}
	return tea.Batch(cmds...)
}

// reqsCmd fetches the selected ngrok tunnel's requests.
func (m Model) reqsCmd() tea.Cmd {
	if m.tab != tabTunnels {
		return nil
	}
	t := m.current().t
	if t == nil || t.API == "" {
		return nil
	}
	tt := *t
	return func() tea.Msg {
		r, _ := scan.NgrokRequests(context.Background(), tt)
		return reqsMsg{tt.API + tt.Name, r}
	}
}

// ------------------------------------------------------------- rows

func (m Model) match(fields ...string) bool {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return true
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

func (m Model) lmatch(l *scan.Listener) bool {
	c := ""
	if l.Docker != nil {
		c = l.Docker.Name + " " + l.Docker.Image
	}
	return m.match(":"+strconv.Itoa(l.Port), l.App, l.Proc, l.Project, c, m.probes[l.Port].Title)
}

func (m Model) items(tab int) []item {
	var out []item
	switch tab {
	case tabDev:
		var docker []item
		for i := range m.ls {
			l := &m.ls[i]
			if !m.lmatch(l) {
				continue
			}
			if l.Dev {
				out = append(out, item{l: l})
			} else if l.Docker != nil {
				docker = append(docker, item{l: l})
			}
		}
		if len(docker) > 0 {
			out = append(out, item{heading: "from docker"})
			out = append(out, docker...)
		}
	case tabAll:
		for i := range m.ls {
			if m.lmatch(&m.ls[i]) {
				out = append(out, item{l: &m.ls[i]})
			}
		}
	case tabDocker:
		for i := range m.ls {
			if m.ls[i].Docker != nil && m.lmatch(&m.ls[i]) {
				out = append(out, item{l: &m.ls[i]})
			}
		}
	case tabTunnels:
		var hidden []item
		for i := range m.tunnels {
			t := &m.tunnels[i]
			if !m.match(t.Tool, t.Public, t.Local, strconv.Itoa(t.Port)) {
				continue
			}
			if t.Seen() {
				out = append(out, item{t: t})
			} else {
				hidden = append(hidden, item{t: t})
			}
		}
		if len(hidden) > 0 {
			out = append(out, item{heading: "running, public URL not known"})
			out = append(out, hidden...)
		}
	}
	return out
}

func (m Model) count(tab int) int {
	n := 0
	for _, it := range m.items(tab) {
		if it.heading == "" && (tab != tabDev || it.l.Dev) {
			n++
		}
	}
	return n
}

func (m Model) current() item {
	its := m.items(m.tab)
	s := m.sel[m.tab]
	if s >= 0 && s < len(its) {
		return its[s]
	}
	return item{}
}

// clamp keeps the selection on a row that exists, off headings.
func (m *Model) clamp() {
	its := m.items(m.tab)
	s := min(m.sel[m.tab], len(its)-1)
	s = max(s, 0)
	for s < len(its) && its[s].heading != "" {
		s++
	}
	if s >= len(its) {
		s = max(0, len(its)-1)
		for s > 0 && its[s].heading != "" {
			s--
		}
	}
	m.sel[m.tab] = s
}

func (m *Model) move(d int) {
	its := m.items(m.tab)
	s := m.sel[m.tab]
	for n := s + d; n >= 0 && n < len(its); n += d {
		if its[n].heading == "" {
			s = n
			break
		}
	}
	m.sel[m.tab] = s
	m.reqSel = 0
}

// tunnelFor is the tunnel to a port, if any.
func (m Model) tunnelFor(port int) *scan.Tunnel {
	for i := range m.tunnels {
		if m.tunnels[i].Port == port && m.tunnels[i].Seen() {
			return &m.tunnels[i]
		}
	}
	return nil
}

func (m Model) listenerOn(port int) *scan.Listener {
	for i := range m.ls {
		if m.ls[i].Port == port {
			return &m.ls[i]
		}
	}
	return nil
}

// ------------------------------------------------------------- keys

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	switch m.pop {
	case popHelp:
		switch s {
		case "j", "down":
			m.helpTop++
		case "k", "up":
			m.helpTop = max(0, m.helpTop-1)
		default:
			m.pop = popNone
		}
		return m, nil
	case popConfirm:
		c := m.conf
		m.pop, m.conf = popNone, nil
		if s == "y" || s == "Y" {
			return m, c.act
		}
		return m, nil
	case popShare:
		switch s {
		case "esc", "q":
			m.pop = popNone
		case "j", "down":
			m.shareAt = min(len(m.share)-1, m.shareAt+1)
		case "k", "up":
			m.shareAt = max(0, m.shareAt-1)
		case "enter":
			return m.startShare(m.shareAt)
		default:
			if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(m.share) {
				return m.startShare(n - 1)
			}
		}
		return m, nil
	}
	if m.filtering {
		switch s {
		case "esc":
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
		case "enter":
			m.filtering = false
			m.filter.Blur()
		case "up", "down", "ctrl+n", "ctrl+p":
			m.move(map[string]int{"up": -1, "ctrl+p": -1}[s]*2 + 1)
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(k)
			m.sel[m.tab] = 0
			m.clamp()
			return m, cmd
		}
		return m, nil
	}
	if m.opt.PopupKey != "" && s == m.opt.PopupKey {
		return m, tea.Quit
	}
	switch s {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.clamp()
			return m, nil
		}
		return m, tea.Quit
	case "?":
		m.pop, m.helpTop = popHelp, 0
		return m, nil
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "j", "down":
		m.move(1)
		return m, m.reqsCmd()
	case "k", "up":
		m.move(-1)
		return m, m.reqsCmd()
	case "home":
		m.sel[m.tab] = 0
		m.clamp()
		return m, m.reqsCmd()
	case "end":
		m.sel[m.tab] = len(m.items(m.tab)) - 1
		m.clamp()
		return m, m.reqsCmd()
	case "ctrl+d", "pgdown":
		for i := 0; i < m.listRows()/2; i++ {
			m.move(1)
		}
		return m, nil
	case "ctrl+u", "pgup":
		for i := 0; i < m.listRows()/2; i++ {
			m.move(-1)
		}
		return m, nil
	case "tab", "l", "right":
		return m.setTab((m.tab + 1) % 4)
	case "shift+tab", "h", "left":
		return m.setTab((m.tab + 3) % 4)
	case "1", "2", "3", "4":
		return m.setTab(int(s[0] - '1'))
	case "a":
		return m.setTab(tabAll)
	case "r":
		return m, tea.Batch(m.scan(), m.say("rescanning…", nil))
	}
	it := m.current()
	if it.t != nil {
		return m.tunnelKey(s, it.t)
	}
	if it.l != nil {
		return m.portKey(s, it.l)
	}
	return m, nil
}

func (m Model) setTab(t int) (tea.Model, tea.Cmd) {
	m.tab = t
	m.reqSel = 0
	m.clamp()
	return m, m.reqsCmd()
}

func (m Model) portKey(s string, l *scan.Listener) (tea.Model, tea.Cmd) {
	switch s {
	case "enter", "o":
		return m, openURL(l.URL())
	case "y":
		return m, copyText(l.URL())
	case "Y":
		if !l.Public() || m.ip == "" {
			return m, m.say(fmt.Sprintf(":%d only listens on this machine (start it with --host to reach it from others)", l.Port), nil)
		}
		return m, copyText("http://" + m.ip + ":" + strconv.Itoa(l.Port))
	case "g":
		p, ok := m.panes[l.PID]
		if !ok {
			return m, m.say("it doesn't run in a tmux pane", nil)
		}
		return m, jumpTo(p, m.opt.Client)
	case "t":
		m.share = scan.Sharers()
		if len(m.share) == 0 {
			return m, m.say("no tunnel tool installed: ngrok, cloudflared, tailscale, lt (localtunnel) or bore", nil)
		}
		m.pop, m.shareP, m.shareAt = popShare, l.Port, 0
		return m, nil
	case "T":
		t := m.tunnelFor(l.Port)
		if t == nil {
			return m, m.say(fmt.Sprintf(":%d has no tunnel: t starts one", l.Port), nil)
		}
		m.tab = tabTunnels
		for i, it := range m.items(tabTunnels) {
			if it.t != nil && it.t.Port == l.Port {
				m.sel[tabTunnels] = i
			}
		}
		return m, m.reqsCmd()
	case "x", "K":
		return m.askStop(l, s == "K")
	}
	return m, nil
}

func (m Model) askStop(l *scan.Listener, force bool) (tea.Model, tea.Cmd) {
	if l.Docker != nil {
		c := *l.Docker
		m.conf = &confirm{title: "stop " + c.Name + "?", body: "docker stop " + c.Name + " (" + c.Image + "); docker start brings it back.",
			yes: "stop the container", color: m.st.T.Peach, act: func() tea.Msg {
				out, err := exec.Command("docker", "stop", c.ID).CombinedOutput()
				if err != nil {
					return doneMsg{err: fmt.Errorf("docker stop: %s", strings.TrimSpace(string(out)))}
				}
				return doneMsg{text: "stopped " + c.Name}
			}}
		m.pop = popConfirm
		return m, nil
	}
	if l.PID == 0 {
		return m, m.say(fmt.Sprintf(":%d belongs to another user or root: pattan can't stop it", l.Port), nil)
	}
	pid, port, name := l.PID, l.Port, l.App
	sig, how, color := syscall.SIGTERM, "asks it to stop (SIGTERM)", m.st.T.Peach
	if force {
		sig, how, color = syscall.SIGKILL, "kills it at once (SIGKILL): unsaved work is lost", m.st.T.Red
	}
	m.conf = &confirm{title: fmt.Sprintf("stop %s on :%d?", name, port), body: fmt.Sprintf("pid %d · %s", pid, how), yes: "stop it", color: color,
		act: func() tea.Msg {
			if err := syscall.Kill(pid, sig); err != nil {
				return doneMsg{err: fmt.Errorf("couldn't stop pid %d: %v", pid, err)}
			}
			return doneMsg{text: fmt.Sprintf("stopped %s (:%d)", name, port)}
		}}
	m.pop = popConfirm
	return m, nil
}

func (m Model) startShare(i int) (tea.Model, tea.Cmd) {
	m.pop = popNone
	if i < 0 || i >= len(m.share) {
		return m, nil
	}
	sh, port := m.share[i], m.shareP
	return m, tea.Batch(m.say(fmt.Sprintf("starting %s for :%d…", sh.Tool, port), nil), func() tea.Msg {
		log, err := scan.Share(sh, port)
		if err != nil {
			return doneMsg{err: fmt.Errorf("%v (log: %s)", err, log)}
		}
		return doneMsg{text: fmt.Sprintf("%s is sharing :%d; it shows in Tunnels once it has a URL (log: %s)", sh.Tool, port, log)}
	})
}

func (m Model) tunnelKey(s string, t *scan.Tunnel) (tea.Model, tea.Cmd) {
	reqs := m.reqs[t.API+t.Name]
	switch s {
	case "enter":
		if !t.Seen() {
			return m, m.say("its public URL isn't known: "+t.Note, nil)
		}
		u := t.Public
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		return m, openURL(u)
	case "o":
		if t.Port == 0 {
			return m, nil
		}
		return m, openURL("http://localhost:" + strconv.Itoa(t.Port))
	case "y":
		if t.Public == "" {
			return m, nil
		}
		return m, copyText(t.Public)
	case "i":
		if t.API == "" {
			return m, m.say("only ngrok has an inspector", nil)
		}
		return m, openURL(t.API)
	case "J":
		m.reqSel = min(max(0, len(reqs)-1), m.reqSel+1)
	case "K":
		m.reqSel = max(0, m.reqSel-1)
	case "R":
		if m.reqSel >= len(reqs) {
			return m, m.say("no request to send again", nil)
		}
		r, tt := reqs[m.reqSel], *t
		return m, func() tea.Msg {
			if err := scan.Replay(context.Background(), tt, r.ID); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "sent again: " + r.Method + " " + r.URI}
		}
	case "x":
		tt := *t
		what := tt.Public
		if what == "" {
			what = tt.Cmd
		}
		m.conf = &confirm{title: "stop the " + tt.Tool + " tunnel?", body: what + " stops being reachable.", yes: "stop it", color: m.st.T.Peach,
			act: func() tea.Msg {
				if err := scan.StopTunnel(tt); err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{text: "stopped the " + tt.Tool + " tunnel"}
			}}
		m.pop = popConfirm
	}
	return m, nil
}

// ------------------------------------------------------------- actions

func openURL(u string) tea.Cmd {
	return func() tea.Msg {
		opener := "xdg-open"
		if b := os.Getenv("BROWSER"); b != "" {
			opener = b
		}
		cmd := exec.Command(opener, u)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return doneMsg{err: fmt.Errorf("couldn't open %s: %v", u, err)}
		}
		go func() { _ = cmd.Wait() }()
		return doneMsg{text: "opened " + u}
	}
}

// copyText puts text on the clipboard: Wayland's, X's, and tmux's (which
// passes it on to the terminal).
func copyText(t string) tea.Cmd {
	return func() tea.Msg {
		ok := false
		try := func(name string, args ...string) {
			if _, err := exec.LookPath(name); err != nil {
				return
			}
			c := exec.Command(name, args...)
			c.Stdin = strings.NewReader(t)
			if c.Run() == nil {
				ok = true
			}
		}
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			try("wl-copy")
		} else if os.Getenv("DISPLAY") != "" {
			try("xclip", "-selection", "clipboard")
		}
		if os.Getenv("TMUX") != "" {
			try("tmux", "load-buffer", "-w", "-")
		}
		if !ok {
			return doneMsg{err: fmt.Errorf("no clipboard tool (wl-copy, xclip or tmux)")}
		}
		return doneMsg{text: "copied " + t}
	}
}

func jumpTo(p scan.Pane, client string) tea.Cmd {
	return func() tea.Msg {
		sess := p.Target[:strings.Index(p.Target, ":")]
		args := []string{"switch-client"}
		if client != "" {
			args = append(args, "-c", client)
		}
		args = append(args, "-t", sess, ";", "select-window", "-t", p.Target[:strings.LastIndex(p.Target, ".")], ";", "select-pane", "-t", p.ID)
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			return doneMsg{err: fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)))}
		}
		return doneMsg{quit: true}
	}
}

// lanIP is this machine's address on the local network.
func lanIP() string {
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || strings.HasPrefix(i.Name, "docker") || strings.HasPrefix(i.Name, "br-") || strings.HasPrefix(i.Name, "veth") || strings.HasPrefix(i.Name, "tailscale") {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				return n.IP.String()
			}
		}
	}
	return ""
}

// ------------------------------------------------------------- view

func (m Model) listRows() int { return max(3, m.h-2-2-1) }

func (m Model) View() string {
	if m.w == 0 {
		return ""
	}
	st := m.st
	var tabs []string
	for i, n := range tabNames {
		tabs = append(tabs, fmt.Sprintf("%s %d", n, m.count(i)))
	}
	right := "󰒋 pattan"
	if !st.Nerd {
		right = "pattan"
	}
	top := st.Tabs(tabs, m.tab, right, m.w)
	bh := m.h - 2
	if m.filtering || m.filter.Value() != "" {
		bh--
	}
	lw := max(40, m.w*55/100)
	rw := m.w - lw
	left := st.Box(lw, bh, m.listTitle(), m.listLines(lw-4, bh-2), true, "")
	rtitle, rlines := m.details(rw - 4)
	right2 := st.Box(rw, bh, rtitle, rlines, false, "")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right2)
	switch m.pop {
	case popHelp:
		box := m.helpBox(bh)
		body = ui.Overlay(body, box, m.w, max(0, (bh-len(box))/2))
	case popConfirm:
		w := min(m.w-4, 72)
		lines := append(ui.Wrap(m.conf.body, w-4), "", st.Key("y", m.conf.yes)+"   "+st.Key("n", "keep it"))
		box := st.Popup(m.conf.title, lines, w, m.conf.color)
		body = ui.Overlay(body, box, m.w, max(0, (bh-len(box))/2))
	case popShare:
		box := m.shareBox()
		body = ui.Overlay(body, box, m.w, max(0, (bh-len(box))/2))
	}
	rows := []string{top}
	if m.filtering || m.filter.Value() != "" {
		rows = append(rows, " "+m.filter.View())
	}
	rows = append(rows, body, m.footer())
	return strings.Join(rows, "\n")
}

func (m Model) listTitle() string {
	n := m.count(m.tab)
	switch m.tab {
	case tabTunnels:
		return fmt.Sprintf("Tunnels · %d", n)
	case tabDocker:
		return fmt.Sprintf("Docker ports · %d", n)
	case tabAll:
		return fmt.Sprintf("All listening · %d", n)
	}
	d := 0
	for _, it := range m.items(tabDev) {
		if it.l != nil && it.l.Dev {
			d++
		}
	}
	return fmt.Sprintf("Dev servers · %d", d)
}

func (m Model) kindIcon(kind string) string {
	st := m.st
	if !st.Nerd {
		return ""
	}
	switch kind {
	case "node":
		return st.Green.Render("󰎙") + " "
	case "python":
		return st.Yellow.Render("\ue73c") + " "
	case "ruby":
		return st.Red.Render("\ue739") + " "
	case "java":
		return st.Peach.Render("\ue738") + " "
	case "php":
		return st.Blue.Render("\ue73d") + " "
	case "go":
		return st.Sky.Render("\ue627") + " "
	case "rust":
		return st.Peach.Render("\ue7a8") + " "
	case "docker":
		return st.Blue.Render("\uf308") + " "
	case "postgres":
		return st.Blue.Render("\ue76e") + " "
	case "redis":
		return st.Red.Render("\ue76d") + " "
	case "web":
		return st.Teal.Render("󰖟") + " "
	case "tunnel":
		return st.Teal.Render("󰖂") + " "
	}
	return st.Muted.Render("󰒋") + " "
}

func (m Model) status(l *scan.Listener) string {
	p, ok := m.probes[l.Port]
	st := m.st
	if !ok {
		return st.Muted.Render("● …")
	}
	switch {
	case p.HTTP && p.Status < 400:
		return st.Green.Render("● " + p.Label())
	case p.HTTP:
		return st.Yellow.Render("● " + p.Label())
	case p.Note == "no answer":
		return st.Red.Render("● no answer")
	}
	return st.Muted.Render("● " + p.Label())
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (m Model) listLines(w, h int) []string {
	st := m.st
	if !m.loaded {
		if m.err != "" {
			return ui.Wrap(m.err, w)
		}
		return []string{st.Muted.Render("looking…")}
	}
	its := m.items(m.tab)
	if len(its) == 0 {
		msg := map[int]string{
			tabDev:     "No dev servers running. Ones you start in a project folder (npm run dev, manage.py runserver...) show up here.",
			tabAll:     "Nothing listens.",
			tabTunnels: "No tunnels. t on a port starts one (ngrok, cloudflared, tailscale funnel, localtunnel, bore); ones you start yourself show up here too.",
			tabDocker:  "No container publishes a port (or Docker isn't running).",
		}[m.tab]
		if m.filter.Value() != "" {
			msg = "Nothing matches “" + m.filter.Value() + "”."
		}
		return ui.Wrap(st.Muted.Render(msg), w)
	}
	var cols []ui.Column
	var rows [][]string
	switch m.tab {
	case tabTunnels:
		cols = []ui.Column{{Title: "tool", Width: 14}, {Title: "public URL", Width: 0}, {Title: "→ local", Width: 18}, {Title: "reqs", Width: 5}, {Title: "up", Width: 4}}
		for _, it := range its {
			if it.heading != "" {
				rows = append(rows, []string{"", st.Muted.Render(strings.ToUpper(it.heading))})
				continue
			}
			t := it.t
			pub := st.Teal.Render(strings.TrimPrefix(t.Public, "https://"))
			if t.Public == "" {
				pub = st.Muted.Render(t.Note)
			} else if t.Tool == "tailscale" && strings.Contains(t.Note, "tailnet") {
				pub = st.Sub.Render(strings.TrimPrefix(t.Public, "https://"))
			}
			local := ""
			if t.Port > 0 {
				local = ":" + strconv.Itoa(t.Port)
				if l := m.listenerOn(t.Port); l != nil && l.App != "" {
					local += " " + l.App
				}
			}
			reqs := ""
			if t.Reqs >= 0 {
				reqs = strconv.Itoa(t.Reqs)
			}
			rows = append(rows, []string{m.tunnelIcon(t.Tool) + st.Bold.Render(t.Tool), pub, local, reqs, st.Muted.Render(ago(t.Started))})
		}
	case tabAll:
		cols = []ui.Column{{Title: "port", Width: 7}, {Title: "on", Width: 8}, {Title: "app", Width: 16}, {Title: "pid", Width: 8}, {Title: "where", Width: 0}}
		for _, it := range its {
			l := it.l
			on := st.Muted.Render("local")
			if l.Public() {
				on = st.Peach.Render("network")
			}
			pid, where := "", st.Muted.Render("another user's")
			if l.PID > 0 {
				pid, where = strconv.Itoa(l.PID), l.Project
			}
			if l.Docker != nil {
				where = l.Docker.Name
			}
			app := l.App
			if app == "" {
				app = st.Muted.Render("?")
			}
			rows = append(rows, []string{st.Bold.Render(":" + strconv.Itoa(l.Port)), on, m.kindIcon(l.Kind) + app, st.Muted.Render(pid), where})
		}
	default: // dev servers, docker
		cols = []ui.Column{{Title: "port", Width: 9}, {Title: "app", Width: 16}, {Title: "project", Width: 0}, {Title: "status", Width: 12}, {Title: "up", Width: 4}}
		if m.tab == tabDocker {
			cols[2].Title = "container"
		}
		for _, it := range its {
			if it.heading != "" {
				rows = append(rows, []string{"", st.Muted.Render(strings.ToUpper(it.heading))})
				continue
			}
			l := it.l
			port := st.Bold.Render(":" + strconv.Itoa(l.Port))
			if t := m.tunnelFor(l.Port); t != nil {
				port += " " + m.tunnelIcon(t.Tool)
			}
			up := ago(l.Started)
			if l.Docker != nil {
				up = dockerUp(l.Docker.Status)
			}
			rows = append(rows, []string{port, m.kindIcon(l.Kind) + l.App, l.Project, m.status(l), st.Muted.Render(up)})
		}
	}
	sel := m.sel[m.tab]
	top := ui.Scroll(sel, m.top[m.tab], h-1)
	return st.Table(cols, rows, sel, top, w, h)
}

// dockerUp shortens docker's "Up 3 days" to "3d".
func dockerUp(status string) string {
	f := strings.Fields(status)
	if len(f) < 3 || f[0] != "Up" {
		return ""
	}
	n := f[1]
	if n == "About" || n == "a" || n == "an" {
		n = "1"
	}
	switch {
	case strings.HasPrefix(f[2], "second"):
		return n + "s"
	case strings.HasPrefix(f[2], "minute"):
		return n + "m"
	case strings.HasPrefix(f[2], "hour"):
		return n + "h"
	case strings.HasPrefix(f[2], "day"):
		return n + "d"
	case strings.HasPrefix(f[2], "week"):
		return n + "w"
	case strings.HasPrefix(f[2], "month"):
		return n + "mo"
	}
	return n
}

func (m Model) tunnelIcon(tool string) string {
	st := m.st
	if !st.Nerd {
		return ""
	}
	switch tool {
	case "ngrok":
		return st.Teal.Render("󰖟") + " "
	case "cloudflared":
		return st.Peach.Render("󰅟") + " "
	case "tailscale":
		return st.Blue.Render("󰖂") + " "
	case "ssh -R":
		return st.Sub.Render("󰒍") + " "
	}
	return st.Sky.Render("󰖟") + " "
}

func home() string { h, _ := os.UserHomeDir(); return h }

func tilde(p string) string {
	if h := home(); h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func mb(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b > 0:
		return fmt.Sprintf("%d MB", b>>20)
	}
	return ""
}

func (m Model) details(w int) (string, []string) {
	it := m.current()
	st := m.st
	switch {
	case it.t != nil:
		return m.tunnelDetails(it.t, w)
	case it.l == nil:
		return "", nil
	}
	l := it.l
	title := fmt.Sprintf(":%d · %s", l.Port, l.App)
	var out []string
	p := m.probes[l.Port]
	switch {
	case p.Title != "":
		out = append(out, st.Bold.Render(p.Title), st.Muted.Render("the page's title, from a quick look at "+l.URL()))
	case p.HTTP:
		out = append(out, st.Bold.Render(fmt.Sprintf("HTTP %d", p.Status)), st.Muted.Render("answers HTTP at "+l.URL()))
	case !p.At.IsZero():
		out = append(out, st.Bold.Render(l.App), st.Muted.Render("listens; "+p.Label()))
	default:
		out = append(out, st.Bold.Render(l.App), "")
	}
	out = append(out, "")
	kv := [][2]string{{"URL", st.Blue.Render(l.URL())}}
	if l.Public() && m.ip != "" {
		kv = append(kv, [2]string{"also on", fmt.Sprintf("http://%s:%d %s", m.ip, l.Port, st.Muted.Render("(your network: Y copies)"))})
	} else {
		kv = append(kv, [2]string{"also on", st.Muted.Render("this machine only")})
	}
	if l.Docker != nil {
		kv = append(kv, [2]string{"container", l.Docker.Name}, [2]string{"image", l.Docker.Image}, [2]string{"status", l.Docker.Status})
	}
	if l.PID > 0 {
		cmd := strings.Join(l.Cmdline, " ")
		kv = append(kv, [2]string{"process", fmt.Sprintf("%s (pid %d)", l.Proc, l.PID)}, [2]string{"command", st.Sub.Render(cmd)})
		kv = append(kv, [2]string{"folder", tilde(l.Cwd)})
		if pane, ok := m.panes[l.PID]; ok {
			kv = append(kv, [2]string{"tmux pane", st.Accent.Render(pane.Target+" · "+pane.Name) + st.Muted.Render("  (g goes there)")})
		}
		run := ago(l.Started)
		if r := mb(l.RSS); r != "" {
			run += " · " + r
		}
		run += fmt.Sprintf(" · %.1f%% CPU", l.CPU)
		kv = append(kv, [2]string{"running", run})
	} else if l.Docker == nil {
		kv = append(kv, [2]string{"process", st.Muted.Render(l.Proc + " (another user's or root's: no details)")})
	}
	kv = append(kv, [2]string{"connections", fmt.Sprintf("%d open", l.Conns)})
	if t := m.tunnelFor(l.Port); t != nil {
		kv = append(kv, [2]string{"public", st.Teal.Render(t.Public) + st.Muted.Render(" ("+t.Tool+", T shows it)")})
	}
	for _, line := range st.KV(kv, 12) {
		out = append(out, ui.Fit(line, w))
	}
	out = append(out, "", st.Muted.Render("enter opens · y copies the URL · g its tmux pane · t shares · x stops"))
	return title, out
}

func (m Model) tunnelDetails(t *scan.Tunnel, w int) (string, []string) {
	st := m.st
	title := t.Tool
	if t.Port > 0 {
		title += fmt.Sprintf(" → :%d", t.Port)
	}
	kv := [][2]string{}
	if t.Public != "" {
		kv = append(kv, [2]string{"public", st.Teal.Render(t.Public)})
	}
	local := t.Local
	if l := m.listenerOn(t.Port); l != nil && t.Port > 0 {
		local = fmt.Sprintf("http://localhost:%d · %s · %s", t.Port, l.App, l.Project)
	}
	kv = append(kv, [2]string{"local", local})
	if t.PID > 0 {
		kv = append(kv, [2]string{"process", fmt.Sprintf("%s %s", st.Sub.Render(t.Cmd), st.Muted.Render(fmt.Sprintf("(pid %d)", t.PID)))})
		kv = append(kv, [2]string{"running", ago(t.Started)})
	}
	if t.API != "" {
		kv = append(kv, [2]string{"inspector", st.Blue.Render(t.API) + st.Muted.Render("  (i opens it)")})
	}
	if t.Note != "" {
		kv = append(kv, [2]string{"note", st.Muted.Render(t.Note)})
	}
	out := st.KV(kv, 10)
	if t.API != "" {
		out = append(out, "", st.Heading("last requests")+st.Muted.Render("  J/K pick · R sends again"))
		reqs := m.reqs[t.API+t.Name]
		if len(reqs) == 0 {
			out = append(out, st.Muted.Render("none yet"))
		}
		for i, r := range reqs {
			code := strconv.Itoa(r.Status)
			switch {
			case r.Status >= 500:
				code = st.Red.Render(code)
			case r.Status >= 400:
				code = st.Yellow.Render(code)
			case r.Status >= 300:
				code = st.Yellow.Render(code)
			default:
				code = st.Green.Render(code)
			}
			bar := "  "
			if i == m.reqSel {
				bar = st.Accent.Render("▌ ")
			}
			line := fmt.Sprintf("%s%s %-6s %s %s", bar, code, r.Method, r.URI, st.Muted.Render(fmt.Sprintf("%d ms · %s", r.Duration.Milliseconds(), r.At.Local().Format("15:04:05"))))
			out = append(out, ui.Fit(line, w))
		}
	}
	out = append(out, "", st.Muted.Render("enter opens the public URL · o the local one · y copies · x stops"))
	return title, out
}

func (m Model) shareBox() []string {
	st := m.st
	w := min(m.w-4, 70)
	l := m.listenerOn(m.shareP)
	name := ""
	if l != nil {
		name = " (" + l.App + ")"
	}
	lines := []string{st.Muted.Render("pick a tunnel tool; it keeps running after pattan quits"), ""}
	for i, s := range m.share {
		bar := "  "
		if i == m.shareAt {
			bar = st.Accent.Render("▌ ")
		}
		lines = append(lines, bar+st.Key(strconv.Itoa(i+1), "")+" "+st.Bold.Render(s.Tool)+"  "+st.Muted.Render(s.Note))
	}
	lines = append(lines, "", st.Muted.Render("enter or the number starts it · esc cancels"))
	return st.Popup(fmt.Sprintf("share :%d%s", m.shareP, name), lines, w, "")
}

func (m Model) helpBox(bh int) []string {
	st := m.st
	w := min(m.w-4, 92)
	var lines []string
	where := "-"
	for _, k := range Keys {
		if k.Where != where {
			where = k.Where
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, st.Heading(map[string]string{"": "everywhere", "ports": "on a port", "tunnels": "on a tunnel"}[where]))
		}
		lines = append(lines, st.Accent.Render(ui.Fit(k.Keys, 17))+" "+k.Desc)
	}
	room := max(3, bh-4)
	top := min(m.helpTop, max(0, len(lines)-room))
	lines = lines[top:min(len(lines), top+room)]
	return st.Popup("keys · any key closes", lines, w, "")
}

func (m Model) footer() string {
	st := m.st
	var keys [][2]string
	mode := "ports"
	switch {
	case m.filtering:
		mode, keys = "filter", [][2]string{{"↵", "keep"}, {"esc", "clear"}}
	case m.tab == tabTunnels:
		mode = "tunnels"
		keys = [][2]string{{"↵", "open public URL"}, {"o", "open local"}, {"y", "copy"}, {"J/K", "requests"}, {"R", "send again"}, {"i", "inspector"}, {"x", "stop"}, {"?", "keys"}}
	default:
		keys = [][2]string{{"j/k", "move"}, {"↵", "open in browser"}, {"y", "copy URL"}, {"g", "its tmux pane"}, {"t", "share"}, {"x", "stop"}, {"a", "all ports"}, {"/", "filter"}, {"?", "keys"}}
	}
	msg := ""
	if m.msg != "" {
		if m.msgErr {
			msg = st.Red.Render(m.msg)
		} else {
			msg = st.Text.Render(m.msg)
		}
	}
	return st.Footer(mode, keys, msg, m.w)
}
