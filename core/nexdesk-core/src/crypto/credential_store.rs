//! Secure credential storage interface (planner task R-16). This crate only
//! defines the interface and an in-memory implementation for tests — a real
//! desktop build must plug in an OS-backed store (Windows Credential
//! Manager/DPAPI, macOS Keychain, libsecret on Linux). That's Electron/
//! platform integration work (Phase 4), not core-crate work; the interface
//! exists now so callers don't need to know which backend is active.

use crate::error::{NexError, Result};
use std::collections::HashMap;
use std::sync::Mutex;

pub trait CredentialStore: Send + Sync {
    fn store(&self, key: &str, secret: &[u8]) -> Result<()>;
    fn load(&self, key: &str) -> Result<Option<Vec<u8>>>;
    fn delete(&self, key: &str) -> Result<()>;
}

/// Test/dev-only implementation — not persisted, not encrypted at rest.
#[derive(Default)]
pub struct InMemoryCredentialStore {
    entries: Mutex<HashMap<String, Vec<u8>>>,
}

impl InMemoryCredentialStore {
    pub fn new() -> Self {
        Self::default()
    }
}

impl CredentialStore for InMemoryCredentialStore {
    fn store(&self, key: &str, secret: &[u8]) -> Result<()> {
        let mut entries = self
            .entries
            .lock()
            .map_err(|_| NexError::Transport("credential store lock poisoned".to_string()))?;
        entries.insert(key.to_string(), secret.to_vec());
        Ok(())
    }

    fn load(&self, key: &str) -> Result<Option<Vec<u8>>> {
        let entries = self
            .entries
            .lock()
            .map_err(|_| NexError::Transport("credential store lock poisoned".to_string()))?;
        Ok(entries.get(key).cloned())
    }

    fn delete(&self, key: &str) -> Result<()> {
        let mut entries = self
            .entries
            .lock()
            .map_err(|_| NexError::Transport("credential store lock poisoned".to_string()))?;
        entries.remove(key);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stores_loads_and_deletes() {
        let store = InMemoryCredentialStore::new();
        assert_eq!(store.load("device-key").unwrap(), None);

        store.store("device-key", b"secret-bytes").unwrap();
        assert_eq!(
            store.load("device-key").unwrap(),
            Some(b"secret-bytes".to_vec())
        );

        store.delete("device-key").unwrap();
        assert_eq!(store.load("device-key").unwrap(), None);
    }
}
