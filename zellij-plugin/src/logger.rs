use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};

static LOGGER: OnceLock<Logger> = OnceLock::new();

pub struct Logger {
    tracing_enabled: AtomicBool,
    plugin_id: Mutex<Option<u32>>,
    session_name: Mutex<String>,
}

impl Logger {
    fn init() -> Self {
        Logger {
            tracing_enabled: AtomicBool::new(false),
            plugin_id: Mutex::new(None),
            session_name: Mutex::new(String::new()),
        }
    }

    pub fn set_plugin_id(&self, id: u32) {
        *self.plugin_id.lock().unwrap() = Some(id);
    }

    pub fn set_session_name(&self, name: String) {
        *self.session_name.lock().unwrap() = name;
    }

    fn prefix(&self) -> String {
        let pid = self.plugin_id.lock().unwrap();
        let name = self.session_name.lock().unwrap();
        match (*pid, name.is_empty()) {
            (Some(id), false) => format!("[p{}/{}] ", id, *name),
            (Some(id), true) => format!("[p{}] ", id),
            (None, false) => format!("[{}] ", *name),
            (None, true) => String::new(),
        }
    }

    pub fn get() -> &'static Logger {
        LOGGER.get_or_init(|| Logger::init())
    }

    pub fn start_tracing(&self) {
        use std::fs::File;
        use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

        let log_path = "/host/plugin.log";
        let file_result = File::create(&log_path);

        match file_result {
            Ok(log_file) => {
                let debug_log = tracing_subscriber::fmt::layer().with_writer(Arc::new(log_file));

                if tracing_subscriber::registry()
                    .with(debug_log)
                    .try_init()
                    .is_ok()
                {
                    self.tracing_enabled.store(true, Ordering::Relaxed);
                    tracing::info!("tracing initialized at {}", log_path);
                } else {
                    eprintln!("[ERROR] error initializing tracing subscriber");
                }
            }
            Err(error) => {
                eprintln!("[ERROR] error creating log file: {:?}", error);
            }
        }
    }

    pub fn debug(&self, message: String) {
        let msg = format!("{}{}", self.prefix(), message);
        if self.tracing_enabled.load(Ordering::Relaxed) {
            tracing::debug!("{}", msg);
        } else {
            eprintln!("[DEBUG] {}", msg);
        }
    }

    pub fn info(&self, message: String) {
        let msg = format!("{}{}", self.prefix(), message);
        if self.tracing_enabled.load(Ordering::Relaxed) {
            tracing::info!("{}", msg);
        } else {
            eprintln!("[INFO] {}", msg);
        }
    }

    pub fn warn(&self, message: String) {
        let msg = format!("{}{}", self.prefix(), message);
        if self.tracing_enabled.load(Ordering::Relaxed) {
            tracing::warn!("{}", msg);
        } else {
            eprintln!("[WARN] {}", msg);
        }
    }

    pub fn error(&self, message: String) {
        let msg = format!("{}{}", self.prefix(), message);
        if self.tracing_enabled.load(Ordering::Relaxed) {
            tracing::error!("{}", msg);
        } else {
            eprintln!("[ERROR] {}", msg);
        }
    }
}

#[macro_export]
macro_rules! log_debug {
    ($($arg:tt)*) => {
        $crate::logger::Logger::get().debug(format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_info {
    ($($arg:tt)*) => {
        $crate::logger::Logger::get().info(format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_warn {
    ($($arg:tt)*) => {
        $crate::logger::Logger::get().warn(format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_error {
    ($($arg:tt)*) => {
        $crate::logger::Logger::get().error(format!($($arg)*))
    };
}
