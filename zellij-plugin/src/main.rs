mod logger;

use logger::Logger;
use serde::{Deserialize, Serialize};
use serde_json;
use std::collections::BTreeMap;
use std::path::PathBuf;
use zellij_tile::prelude::*;

const DAEMON_PORT: &str = match option_env!("UTENA_PORT") {
    Some(p) => p,
    None => "3333",
};

const PIPE_NAME: &str = match option_env!("UTENA_PIPE_NAME") {
    Some(p) => p,
    None => "utena-commands",
};

const DATA_DIR: &str = match option_env!("UTENA_DATA_DIR") {
    Some(p) => p,
    None => "",
};

const TUI_PATH: &str = match option_env!("UTENA_TUI_PATH") {
    Some(p) => p,
    None => "utena",
};

struct State {
    plugin_id: u32,
    tui_open: bool,
    daemon_url: String,
}

impl Default for State {
    fn default() -> Self {
        Self {
            plugin_id: 0,
            tui_open: false,
            daemon_url: format!("http://localhost:{}/zellij/sessions", DAEMON_PORT),
        }
    }
}

#[derive(Serialize, Debug)]
struct SessionUpdate {
    name: String,
    connected_clients: usize,
}

#[derive(Serialize, Debug)]
struct SessionUpdateRequest {
    id: String,
    sessions: Vec<SessionUpdate>,
}

#[derive(Deserialize, Debug)]
struct PluginCommand {
    command: String,
    session_name: Option<String>,
    workspace_path: Option<String>,
}

impl State {
    fn handle_key_event(
        &mut self,
        key: KeyWithModifier,
    ) -> Result<bool, Box<dyn std::error::Error>> {
        log_debug!("Key pressed: {:?}", key);
        Ok(false)
    }

    fn launch_session_picker(&mut self) {
        if self.tui_open {
            log_debug!("Session picker already open, ignoring Ctrl+P");
            return;
        }

        log_info!("Launching session picker TUI from {} (port={}, pipe={})", TUI_PATH, DAEMON_PORT, PIPE_NAME);

        let command = CommandToRun {
            path: PathBuf::from(TUI_PATH),
            args: vec![
                "--port".to_string(), DAEMON_PORT.to_string(),
                "--pipe-name".to_string(), PIPE_NAME.to_string(),
            ],
            cwd: None,
        };

        let mut context = BTreeMap::new();
        context.insert("source".to_string(), "utena-session-picker".to_string());

        open_command_pane_floating(command, None, context);

        self.tui_open = true;
    }

    fn execute_command(&mut self, command: PluginCommand) {
        log_debug!("Executing command: {:?}", command);

        match command.command.as_str() {
            "open_picker" => {
                log_info!("Opening session picker via pipe command");
                self.launch_session_picker();
            }

            "switch_session" => {
                if let Some(session_name) = command.session_name {
                    log_info!("Switching to session: {}", session_name);
                    switch_session_with_cwd(Some(&session_name), None);
                } else {
                    log_error!("switch_session missing session_name");
                }
            }

            "create_session" => {
                if let (Some(session_name), Some(workspace_path)) =
                    (command.session_name, command.workspace_path)
                {
                    log_info!("Creating session: {} at {}", session_name, workspace_path);
                    let cwd = PathBuf::from(workspace_path);
                    switch_session_with_cwd(Some(&session_name), Some(cwd));
                } else {
                    log_error!("create_session missing required fields");
                }
            }

            "close_picker" => {
                log_info!("Closing session picker");
                self.tui_open = false;
            }

            _ => {
                log_warn!("Unknown command: {}", command.command);
            }
        }
    }
}

register_plugin!(State);

impl ZellijPlugin for State {
    fn load(&mut self, _configuration: BTreeMap<String, String>) {
        let ids = get_plugin_ids();
        self.plugin_id = ids.plugin_id;
        Logger::get().set_plugin_id(ids.plugin_id);

        request_permission(&[
            PermissionType::RunCommands,
            PermissionType::ChangeApplicationState,
            PermissionType::ReadApplicationState,
            PermissionType::FullHdAccess,
            PermissionType::WebAccess,
        ]);

        subscribe(&[
            EventType::Key,
            EventType::FileSystemUpdate,
            EventType::SessionUpdate,
            EventType::RunCommandResult,
            EventType::WebRequestResult,
            EventType::PermissionRequestResult,
            EventType::ConfigWasWrittenToDisk,
            EventType::FailedToWriteConfigToDisk,
            EventType::HostFolderChanged,
            EventType::FailedToChangeHostFolder,
            EventType::PaneClosed,
            EventType::PaneUpdate,
        ]);
    }

    fn update(&mut self, event: Event) -> bool {
        let mut should_render = false;

        match event {
            Event::PermissionRequestResult(perms) => {
                hide_self();

                log_debug!("Perms updated: {:#?}", perms);

                let host_dir = if DATA_DIR.is_empty() {
                    format!(
                        "{}/.config/utena",
                        std::env::var("HOME").unwrap_or_default()
                    )
                } else {
                    DATA_DIR.to_string()
                };
                change_host_folder(PathBuf::from(host_dir));

                should_render = false;
            }
            Event::HostFolderChanged(_host_folder) => {
                Logger::get().start_tracing();
            }
            Event::SessionUpdate(sessions, _) => {
                let current_session_name = sessions
                    .iter()
                    .find(|s| s.is_current_session)
                    .map(|s| s.name.clone())
                    .unwrap_or_default();

                Logger::get().set_session_name(current_session_name.clone());

                let session_updates: Vec<SessionUpdate> = sessions
                    .iter()
                    .map(|session| SessionUpdate {
                        name: session.name.clone(),
                        connected_clients: session.connected_clients,
                    })
                    .collect();

                let req = SessionUpdateRequest {
                    id: current_session_name.clone(),
                    sessions: session_updates,
                };

                let body = serde_json::to_vec(&req).unwrap();
                let mut headers = BTreeMap::new();
                headers.insert("X-Zellij-Session".to_string(), current_session_name.clone());
                let context = BTreeMap::new();
                web_request(
                    self.daemon_url.clone(),
                    HttpVerb::Put,
                    headers,
                    body,
                    context,
                );
            }
            Event::Key(key) => match self.handle_key_event(key) {
                Ok(rerender_needed) => should_render = rerender_needed,
                Err(e) => {
                    log_error!("Failed to handle keypress: {:#?}", e);
                    should_render = false;
                }
            },
            Event::WebRequestResult(status, _headers, raw_body, _context) => unsafe {
                if status != 200 {
                    let body = String::from_utf8_unchecked(raw_body);
                    log_error!("Resp body: {:#?}", body)
                }
            },
            Event::RunCommandResult(_exit_code, _stdout, _stderr, _context) => {}
            Event::PaneUpdate(pane_manifest) => {
                log_debug!("PaneUpdate");
                if self.tui_open {
                    let utena_pane = pane_manifest
                        .panes
                        .iter()
                        .flat_map(|(_, pane_infos)| pane_infos.iter())
                        .find(|pane_info| {
                            if let Some(terminal_command) = &pane_info.terminal_command {
                                terminal_command.contains("utena")
                            } else {
                                false
                            }
                        });

                    if let Some(pane_info) = utena_pane {
                        if pane_info.is_held {
                            log_info!("TUI command pane is held, closing pane id={}", pane_info.id);
                            self.tui_open = false;
                            close_terminal_pane(pane_info.id);
                        }
                    }
                }
            }
            Event::PaneClosed(_pane_id) => {
                if self.tui_open {
                    log_debug!("Pane closed, resetting TUI open state");
                    self.tui_open = false;
                }
            }
            _ => {
                log_debug!("Unhandled event: {:#?}", event)
            }
        }

        should_render
    }

    fn pipe(&mut self, pipe_message: PipeMessage) -> bool {
        log_info!(
            "Pipe message received: name={} payload={:?}",
            pipe_message.name,
            pipe_message.payload
        );

        if pipe_message.name != PIPE_NAME {
            return false;
        }

        let payload = match &pipe_message.payload {
            Some(p) => p,
            None => {
                log_warn!("Received pipe message with no payload: {:#?}", pipe_message);
                return false;
            }
        };

        log_debug!("Received pipe message: {}", payload);

        let command: PluginCommand = match serde_json::from_str(payload) {
            Ok(cmd) => cmd,
            Err(e) => {
                log_error!("Failed to parse pipe command: {}", e);
                return false;
            }
        };

        self.execute_command(command);
        false
    }

    fn render(&mut self, _rows: usize, _cols: usize) {}
}
