# Changelog

Newest first. Releases are tags (`vX.Y.Z`).

## Unreleased

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
