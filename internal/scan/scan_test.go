package scan

import (
	"strings"
	"testing"
)

func TestIdentify(t *testing.T) {
	cases := []struct {
		proc, line, app string
	}{
		{"node", "node /home/u/app/node_modules/.bin/vite --host", "vite"},
		{"next-server", "next-server (v15.0.0)", "next"},
		{"python3", "python3 manage.py runserver 0.0.0.0:8000", "django"},
		{"python3", "python3 -m uvicorn app:app --reload", "uvicorn"},
		{"node", "node --require /x/node_modules/tsx/dist/preflight.cjs --import file:///x/loader.mjs bin/ekatva.js daemon", "ekatva"},
		{"ruby", "ruby bin/rails server", "rails"},
		{"hugo", "hugo server -D", "hugo"},
		{"mystery", "mystery --serve", "mystery"},
	}
	for _, c := range cases {
		app, _ := Identify(c.proc, strings.Fields(c.line), "/nonexistent")
		if app != c.app {
			t.Errorf("%q: got %q, want %q", c.line, app, c.app)
		}
	}
}

func TestProject(t *testing.T) {
	if got := Project(home + "/codes/onads-super/apps/web"); got != "onads-super/apps/web" {
		t.Errorf("got %q", got)
	}
	if got := Project(home + "/notes/site"); got != "~/notes/site" {
		t.Errorf("got %q", got)
	}
}

func TestSSHRemote(t *testing.T) {
	args := strings.Fields("ssh -N -R 8080:localhost:3000 -R 0.0.0.0:9000:127.0.0.1:4000 -p 2222 me@vps.example.com")
	f := sshRemote(args)
	if len(f) != 2 || f[0] != (sshFwd{8080, 3000}) || f[1] != (sshFwd{9000, 4000}) {
		t.Fatalf("got %v", f)
	}
	if d := sshDest(args); d != "vps.example.com" {
		t.Errorf("dest %q", d)
	}
	if tunnelTool("ssh", args) != "ssh -R" || tunnelTool("ssh", strings.Fields("ssh host")) != "" {
		t.Error("ssh -R not told apart from plain ssh")
	}
}

func TestPortOf(t *testing.T) {
	for in, want := range map[string]int{"http://localhost:5173": 5173, "localhost:3000/": 3000, "8080": 8080, "http://app.local": 80} {
		if got := portOf(in); got != want {
			t.Errorf("%q: got %d", in, got)
		}
	}
}
