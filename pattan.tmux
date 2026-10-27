#!/usr/bin/env bash
# pattan as a tmux plugin: TPM entry point (also works with a plain
# `run-shell` on this file). Binds prefix + @pattan-key to open pattan (your
# ports, Docker's, and their tunnels) in a popup; the same key inside the
# popup closes it.
#
# Options (set them before TPM's `run` line):
#   @pattan-key       key after the prefix (default P; "none" binds nothing)
#   @pattan-width     popup width  (default 90%)
#   @pattan-height    popup height (default 88%)
#   @pattan-title     popup title  (default " pattan ")
#   @pattan-launcher  your own popup command instead of display-popup: it is
#                     run with display-popup's arguments appended (-E -d DIR
#                     -T TITLE CMD), tmux formats expanded, e.g.
#                     "~/.tmux/scripts/popup.sh '#{client_name}'"
#
# To open it from a status-line click, open a popup running
# "<plugin dir>/scripts/pattan-run --client '#{client_name}'".

dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
run="$dir/scripts/pattan-run"

opt() { local v; v=$(tmux show-option -gqv "$1"); printf '%s' "${v:-$2}"; }
key=$(opt @pattan-key P)
[ "$key" = none ] && exit 0
width=$(opt @pattan-width 90%)
height=$(opt @pattan-height 88%)
title=$(opt @pattan-title ' pattan ')
launcher=$(opt @pattan-launcher '')

cmd="'$run' --popup '$key' --client '#{client_name}'"
if [ -n "$launcher" ]; then
  open="run -b \"$launcher -E -d '#{pane_current_path}' -T '$title' $cmd\""
else
  open="display-popup -E -w '$width' -h '$height' -d '#{pane_current_path}' -T '$title' \"$cmd\""
fi

tmux bind-key -N "Open pattan ports popup (dev servers, Docker ports, tunnels; prefix $key again closes)" "$key" "$open"
