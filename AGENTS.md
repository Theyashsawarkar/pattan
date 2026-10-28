# pattan -- notes for AI agents (same as CLAUDE.md)

The ports your dev servers listen on, Docker's published ports and the
tunnels that make them public: one Go binary (`cmd/pattan`) with a Bubble
Tea screen and a small CLI. Usable as the `pattan` command (install.sh,
make install) and as a TPM plugin (`pattan.tmux` binds a popup key). It is
pulled into Vayu (github.com/Theyashsawarkar/vayu) as a tmux plugin, but
must work on any Linux setup without Vayu: no hardcoded paths,
Vayu-specific behaviour only through the `@pattan-*` options.

- Layout: `internal/scan` finds things (ss and /proc for ports, docker ps,
  HTTP probes, tunnels); `internal/tui` the screen; `internal/ui` the
  drawing kit shared in look with sandesh and nauka (rounded boxes, tabs,
  key caps, varna colours).
- Keys live in `internal/tui/keys.go`. After adding, changing or removing
  one: `go test ./internal/tui -update` (rewrites `scripts/keybindings`,
  which Vayu's keybinding search reads), and update the README's key
  tables. Descriptions start with a verb the search knows.
- Never send HTTP to a database's port (see `SkipHTTP`): it only fills its
  logs with errors.
- The README documents every feature, key, option and command: a change to
  any of them updates it. Every change gets a CHANGELOG.md entry; releases
  are tags (`vX.Y.Z`) with `Version` in cmd/pattan/main.go bumped to match.
- `make check` must pass (vet, gofmt, ShellCheck, tests).
- Test on a throwaway tmux server, never the user's live one:
  `tmux -L t -f /dev/null new -d -s o -x 150 -y 40 "env -u TMUX bin/pattan"`,
  then `tmux -L t send-keys -t o ...` / `capture-pane -p -t o`, and
  `tmux -L t kill-server` after. Never stop the user's servers or start a
  real tunnel in a test.
- The website (docs/, GitHub Pages) is generated: after changing README.md, CHANGELOG.md or docs/adr/, run `python3 scripts/build-site.py` and commit docs/ (CI checks it). A significant design decision gets a new ADR in docs/adr/ (next number; supersede, never rewrite).
