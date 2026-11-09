# Changelog

Newest first. Releases are tags (`vX.Y.Z`).

## v0.2.0 (2026-11-09)

- Vim keys, the grammar every Vayu tool now shares (from the new shared
  module vyakarana): NORMAL and INSERT modes (`/` filters, `Esc` goes back
  and keeps the filter), counts (`5j`, `3gg`), `g g` / `G`, `H` / `L` for
  tabs, `dd` to stop, and a `Space` leader with a which-key box after
  300 ms: `Space t` tabs, `Space y` copy, `Space d` stop, `Space g` go to,
  `Space a t` share, `Space i` ngrok's inspector. `Space Space` searches
  every action; `?` shows every key and no longer closes on a stray key.
  Stopping asks first and only `y` says yes.
- Moved keys say where they went: `t` → `Space a t`, `T` → `Space g t`,
  `K` (force stop) → `Space d k`, `a` → `Space t a`; on a tunnel `R` →
  `Space i r`, `i` → `Space i i`. `g` alone no longer jumps to the tmux
  pane (`Space g p`); `1`–`4` are counts (`Space t 1`–`4` switch tabs).
- The README's key tables and `scripts/keybindings` are generated from the
  keymap; the search shows each key's view and mode (`pattan ports:normal`).
- The drawing kit (`internal/ui`) now comes from vyakarana instead of a copy.
- Keys typed in a fast burst (`gg`, `dd`, `3j`, `Space Space res`) work:
  Bubble Tea hands letters that arrive together as one key, and they were
  dropped. A confirm that opens drops the rest of the burst, so a fast
  `ddy` still asks. In a tmux popup, its key (`P`) closes pattan from the
  `?` sheet too.
- Every corner is rounded: the website's callouts.
- A logo and a shloka at the top of the README and the website: every Vayu tool has them now.

- Screenshots in the README and on the website.
- A website (GitHub Pages, https://theyashsawarkar.github.io/pattan/): the README as a home page, the changelog and architecture decision records (`docs/adr/`), built by `scripts/build-site.py` like the other Vayu tools' sites; CI checks it's up to date.

## v0.1.1 (2026-10-27)

- The built binary no longer holds a line Vayu's keybinding search took
  for a broken key description (it scans plugins' `bin/` folders).

## v0.1.0 (2026-10-27)

- First release: dev servers, every listening port, Docker's published
  ports and tunnels (ngrok, cloudflared, tailscale serve/funnel, ssh -R,
  localtunnel, bore, zrok) in one screen. Enter opens a port in the
  browser; `g` jumps to its tmux pane; `t` starts a tunnel; `x` / `K`
  stop a server; ngrok's last requests can be sent again (`R`).
- The app is named from the command line, then the project's
  `package.json`; databases are checked for an open socket only, never
  sent HTTP.
- tmux plugin: `prefix P` opens it in a popup.
