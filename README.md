# pattan

**The ports your dev servers listen on, Docker's published ports, and the
tunnels that make them public, in the terminal.** Enter opens
`localhost:port` in the browser.

pattan (पत्तन) is Sanskrit for a harbour: where things come in and go out.

```
 Dev servers 5   All listening 13   Tunnels 1   Docker 4                    󰒋 pattan
╭─ Dev servers · 5 ───────────────────────────╮╭─ :5173 · vite ──────────────────────────╮
│ PORT     APP          PROJECT        STATUS  ││ OnAds · Dashboard                       │
│▌:5173 󰖟  vite         onads/apps/web ● 200 OK││                                         │
│ :4000    express      onads/apps/api ● 404   ││ URL        http://localhost:5173        │
│ :4321    astro        onads/apps/docs● 200 OK││ also on    http://192.168.1.12:5173     │
│          FROM DOCKER                         ││ process    node (pid 48211)             │
│ :5432    postgres     onads-postgres-1 ● tcp ││ tmux pane  main:2.1 · claude            │
╰──────────────────────────────────────────────╯╰─────────────────────────────────────────╯
 ports   j/k move   ↵ open in browser   y copy URL   g its tmux pane   t share   x stop
```

## What it shows

- **Dev servers**: the TCP ports your own processes listen on from inside
  a project folder (somewhere under your home, not a hidden folder), so
  daemons like KDE Connect or mpd stay out. Each with the app (vite, next,
  nest, astro, django, uvicorn, rails, hugo... read from the command line,
  then from the project's `package.json`), the project folder, whether it
  answers (HTTP status, or `tcp` for databases, which are never sent HTTP),
  and how long it has run. Docker's published ports follow, under *from
  docker*.
- The details pane: the page's `<title>`, the URL, the address other
  machines on your network can use (when it listens on more than
  loopback), the process and its command, the folder, the **tmux pane it
  runs in**, uptime, memory, CPU, open connections, and its public tunnel
  URL if it has one.
- **All listening**: every listening port on the machine, local or open to
  the network, with the process when it's yours to see.
- **Tunnels**: every tunnel running, with its public URL and the local
  server it points at:
  - **ngrok**: from its local API (`localhost:4040`): the public URL, the
    request count, and the **last requests**; `R` sends one again (handy
    for webhooks), `i` opens ngrok's inspector.
  - **cloudflared** quick tunnels: the `trycloudflare.com` URL, from its
    metrics server.
  - **tailscale serve / funnel**: what this machine serves, and whether
    it's public (funnel) or tailnet-only.
  - **ssh -R** forwards: the remote host and port.
  - **localtunnel**, **bore**, **zrok**: found by their process; the URL
    when their command line gives it.
- **Docker**: the ports containers publish.

`t` on a port starts a tunnel to it with any tool you have installed
(ngrok, cloudflared, tailscale funnel, localtunnel, bore). It runs on its
own, keeps running after pattan quits, and logs to
`~/.local/state/pattan/<tool>-<port>.log`.

Everything refreshes every 2 seconds; the HTTP check every 10.

## Keys

| Key | Does |
|---|---|
| `j` / `k` | Move to the next / previous port |
| `Home` / `End` | Jump to the first / last |
| `Tab` / `Shift+Tab`, `h` / `l`, `1`–`4` | Switch tabs |
| `a` | Show every listening port |
| `/` | Search the list (port, app, project, URL); `Esc` clears it |
| `r` | Rescan now |
| `?` | Show every key |
| `q` / `Esc` | Quit |

On a port:

| Key | Does |
|---|---|
| `Enter` | Open `localhost:port` in the browser |
| `y` / `Y` | Copy the URL / the URL for your local network |
| `g` | Jump to the tmux pane the server runs in |
| `t` | Start a tunnel to it (share it) |
| `T` | Show its tunnel in the Tunnels tab |
| `x` | Stop it (SIGTERM; a container: `docker stop`), asks first |
| `K` | Force stop it (SIGKILL), asks first |

On a tunnel:

| Key | Does |
|---|---|
| `Enter` | Open the public URL |
| `o` | Open the local server it points at |
| `y` | Copy the public URL |
| `J` / `K` | Move through ngrok's last requests |
| `R` | Send the selected request again (ngrok replay) |
| `i` | Open ngrok's inspector |
| `x` | Stop the tunnel, asks first |

## Install

Needs Linux with `ss` (iproute2). Docker, tmux and the tunnel tools are
optional: pattan uses what's there.

```sh
curl -fsSL https://raw.githubusercontent.com/Theyashsawarkar/pattan/main/install.sh | bash
```

downloads the release binary (or builds it with Go) into
`~/.local/share/pattan` and links `~/.local/bin/pattan`. Or `make install`
(`PREFIX`, `DESTDIR`), or `go install github.com/Theyashsawarkar/pattan/cmd/pattan@latest`.

### As a tmux plugin

```tmux
set -g @plugin 'Theyashsawarkar/pattan'
```

`prefix P` opens pattan in a popup; `P` inside closes it. Without pattan on
your `PATH`, the plugin builds it with Go the first time.

| Option | Default | |
|---|---|---|
| `@pattan-key` | `P` | the key after the prefix; `none` binds nothing |
| `@pattan-width` / `@pattan-height` | `90%` / `88%` | popup size |
| `@pattan-title` | ` pattan ` | popup title |
| `@pattan-launcher` | | your own popup command instead of `display-popup`; it gets display-popup's arguments (`-E -d DIR -T TITLE CMD`) |

To open it from a status-line click, open a popup that runs
`~/.tmux/plugins/pattan/scripts/pattan-run --client '#{client_name}'`.

## Command line

```
pattan                 the screen
pattan list            the dev servers' ports: "5173 vite onads-super/apps/web"
pattan list --all      every listening port
pattan tunnels         the tunnels: "ngrok https://… -> :5173"
pattan --version
```

## Colours

pattan follows [varna](https://github.com/Theyashsawarkar/varna)'s desktop
theme (`~/.local/state/varna/palette.json`, a `tools.pattan` pick if the
theme has one) and changes live with it; without varna it is Catppuccin
Mocha. Icons need a Nerd Font; `NO_NERD_FONT=1` turns them off.

## Privacy

pattan reads `/proc` and `ss`, asks your own ports over localhost, and
talks to ngrok's and cloudflared's local APIs. Nothing leaves the machine
unless you start a tunnel, which makes that one port public.

## License

MIT
