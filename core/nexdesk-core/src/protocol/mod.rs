//! Protobuf codec, frame reassembly, jitter/backpressure (planner tasks R-19..R-23).

pub mod backpressure;
pub mod codec;
pub mod jitter;
pub mod reassembly;

pub use nexdesk_proto::nexdesk::v1::SessionHello;

#[cfg(test)]
mod tests {
    use super::*;
    use prost::Message;

    #[test]
    fn session_hello_round_trips() {
        let hello = SessionHello {
            protocol_version: 1,
            device_id: "test-device".to_string(),
        };
        let bytes = hello.encode_to_vec();
        let decoded = SessionHello::decode(bytes.as_slice()).unwrap();
        assert_eq!(hello, decoded);
    }
}
