#!/usr/bin/env bash

plugin_dir=${TMUX_PLUGIN_MANAGER_PATH:-$HOME/.config/tmux/plugins}
plugin_path="$plugin_dir/utena-tmux"
script_dir="$plugin_path/scripts"

tmux set -g status on
tmux set -g status-interval 1
tmux set -g status-left-length 200
tmux set -g status-left "#(utena status-line)"
tmux set -g focus-events on

tmux set-hook -g session-created "run-shell '${script_dir}/hook.sh session-created \"#{hook_session_name}\"'"
tmux set-hook -g session-closed "run-shell '${script_dir}/hook.sh session-closed \"#{hook_session_name}\"'"
tmux set-hook -g client-session-changed "run-shell '${script_dir}/hook.sh client-session-changed \"#{session_name}\"'"
tmux set-hook -g client-attached "run-shell '${script_dir}/hook.sh client-attached \"#{session_name}\"'"
tmux set-hook -g client-detached "run-shell '${script_dir}/hook.sh client-detached \"#{session_name}\"'"

tmux set-hook -g after-new-window "run-shell '${script_dir}/sync-windows.sh \"#{session_name}\"'"
tmux set-hook -g window-renamed "run-shell '${script_dir}/sync-windows.sh \"#{session_name}\"'"
tmux set-hook -g after-kill-window "run-shell '${script_dir}/sync-windows.sh \"#{session_name}\"'"
tmux set-hook -g after-select-window "run-shell '${script_dir}/sync-windows.sh \"#{session_name}\"'"

tmux bind-key p display-popup -E -w 80% -h 80% "utena"
tmux bind-key t display-popup -E -w 80% -h 80% "utena new-task"
