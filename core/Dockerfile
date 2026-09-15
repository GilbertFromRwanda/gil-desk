# Proves the Rust<->C++ FFI boundary actually links and executes (Gate G0),
# using the `real-codec-link` feature (see core/ffi/Cargo.toml). rust:bookworm
# ships a C++ compiler already; libx264/libavcodec dev packages (for the
# real encoder/decoder) still need installing explicitly.
FROM rust:1-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libx264-dev libavcodec-dev libavutil-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY core ./core
COPY codec ./codec
WORKDIR /src/core
RUN cargo test --workspace --features nexdesk-ffi/real-codec-link
