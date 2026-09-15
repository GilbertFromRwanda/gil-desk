# Protobuf codegen tools (planner task F-11). Reproducible codegen without
# requiring protoc / plugins installed on every developer machine — run via
# scripts/gen-proto.sh (or .ps1), not part of the normal cargo/go build.

FROM rust:1-bookworm AS rust-plugin
RUN cargo install protoc-gen-prost --locked

FROM golang:1.25-bookworm AS go-plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    protobuf-compiler && rm -rf /var/lib/apt/lists/*
COPY --from=rust-plugin /usr/local/cargo/bin/protoc-gen-prost /usr/local/bin/
COPY --from=go-plugins /go/bin/protoc-gen-go /usr/local/bin/
COPY --from=go-plugins /go/bin/protoc-gen-go-grpc /usr/local/bin/
WORKDIR /work
ENTRYPOINT ["protoc"]
