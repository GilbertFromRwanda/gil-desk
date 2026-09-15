//! Transport abstraction over TCP/QUIC (planner tasks R-07..R-12).

pub trait Transport: Send + Sync {
    fn name(&self) -> &'static str;
}

#[cfg(test)]
mod tests {
    use super::*;

    struct NullTransport;
    impl Transport for NullTransport {
        fn name(&self) -> &'static str {
            "null"
        }
    }

    #[test]
    fn placeholder_transport_reports_name() {
        assert_eq!(NullTransport.name(), "null");
    }
}
