#!/usr/bin/env bash

plugin_dir=${TMUX_PLUGIN_MANAGER_PATH:-$HOME/.config/tmux/plugins}
plugin_path="$plugin_dir}utena-tmux"

tmux set-hook -g session-created "run-shell '${plugin_path}/scripts/hook.sh session-created \"#{hook_session_name}\"'"
tmux set-hook -g session-closed "run-shell '${plugin_path}/scripts/hook.sh session-closed \"#{hook_session_name}\"'"
tmux set-hook -g client-session-changed "run-shell '${plugin_path}/scripts/hook.sh client-session-changed \"#{session_name}\"'"
tmux set-hook -g client-attached "run-shell '${plugin_path}/scripts/hook.sh client-attached \"#{session_name}\"'"
tmux set-hook -g client-detached "run-shell '${plugin_path}/scripts/hook.sh client-detached \"#{session_name}\"'"
tmux bind-key p display-popup -E -w 80% -h 80% "utena"
