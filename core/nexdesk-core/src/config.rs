//! Configuration loader (planner task R-03). Env-var based for now —
//! deliberately no file/serde dependency yet; add one only when a real
//! deployment needs it (see NexDesk_Improved_Planner.md's "no speculative
//! abstractions" spirit).

use std::time::Duration;

#[derive(Debug, Clone, PartialEq)]
pub struct Config {
    pub bind_addr: String,
    pub connect_timeout: Duration,
    pub heartbeat_interval: Duration,
    pub max_reconnect_attempts: u32,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            bind_addr: "0.0.0.0:7000".to_string(),
            connect_timeout: Duration::from_secs(10),
            heartbeat_interval: Duration::from_secs(15),
            max_reconnect_attempts: 5,
        }
    }
}

impl Config {
    /// Loads from `NEXDESK_*` env vars, falling back to defaults for any
    /// that are unset. Returns an error if a set var can't be parsed.
    pub fn from_env() -> crate::error::Result<Self> {
        let mut config = Self::default();

        if let Ok(v) = std::env::var("NEXDESK_BIND_ADDR") {
            config.bind_addr = v;
        }
        if let Ok(v) = std::env::var("NEXDESK_CONNECT_TIMEOUT_SECS") {
            config.connect_timeout = Duration::from_secs(parse_u64("NEXDESK_CONNECT_TIMEOUT_SECS", &v)?);
        }
        if let Ok(v) = std::env::var("NEXDESK_HEARTBEAT_INTERVAL_SECS") {
            config.heartbeat_interval =
                Duration::from_secs(parse_u64("NEXDESK_HEARTBEAT_INTERVAL_SECS", &v)?);
        }
        if let Ok(v) = std::env::var("NEXDESK_MAX_RECONNECT_ATTEMPTS") {
            config.max_reconnect_attempts = v.parse().map_err(|_| {
                crate::error::NexError::Config(format!(
                    "NEXDESK_MAX_RECONNECT_ATTEMPTS: invalid u32 {v:?}"
                ))
            })?;
        }

        Ok(config)
    }
}

fn parse_u64(var: &str, value: &str) -> crate::error::Result<u64> {
    value
        .parse()
        .map_err(|_| crate::error::NexError::Config(format!("{var}: invalid u64 {value:?}")))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_sane() {
        let config = Config::default();
        assert_eq!(config.bind_addr, "0.0.0.0:7000");
        assert_eq!(config.max_reconnect_attempts, 5);
    }

    #[test]
    fn rejects_unparseable_override() {
        // SAFETY: single-threaded test process; no other test reads this var.
        unsafe { std::env::set_var("NEXDESK_MAX_RECONNECT_ATTEMPTS", "not-a-number") };
        let result = Config::from_env();
        unsafe { std::env::remove_var("NEXDESK_MAX_RECONNECT_ATTEMPTS") };
        assert!(result.is_err());
    }
}
