//! Generic protobuf encode/decode helpers (planner task R-19). Thin
//! wrappers so callers don't each repeat `.encode_to_vec()` /
//! `M::decode(...)` with their own ad hoc error mapping.

use crate::error::{NexError, Result};
use prost::Message;

pub fn encode<M: Message>(msg: &M) -> Vec<u8> {
    msg.encode_to_vec()
}

pub fn decode<M: Message + Default>(bytes: &[u8]) -> Result<M> {
    M::decode(bytes).map_err(|e| NexError::Transport(format!("protobuf decode failed: {e}")))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::SessionHello;

    #[test]
    fn round_trips_through_the_generic_helpers() {
        let hello = SessionHello {
            protocol_version: 1,
            device_id: "test-device".to_string(),
        };
        let bytes = encode(&hello);
        let decoded: SessionHello = decode(&bytes).unwrap();
        assert_eq!(hello, decoded);
    }

    #[test]
    fn decode_reports_a_clean_error_on_garbage_input() {
        let result: Result<SessionHello> = decode(&[0xff, 0xff, 0xff]);
        assert!(result.is_err());
    }
}
