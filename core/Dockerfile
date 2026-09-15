# Proves the Rust<->C++ FFI boundary actually links and executes (Gate G0),
# using the `real-codec-link` feature (see core/ffi/Cargo.toml). rust:bookworm
# ships a C++ compiler already, so no extra toolchain install is needed here.
FROM rust:1-bookworm
WORKDIR /src
COPY core ./core
COPY codec ./codec
WORKDIR /src/core
RUN cargo test --workspace --features nexdesk-ffi/real-codec-link
