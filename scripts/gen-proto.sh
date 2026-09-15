#!/usr/bin/env bash
# Regenerate Rust + Go code from proto/nexdesk/v1/*.proto (planner task F-11).
# Runs protoc in a container so protoc/plugins don't need to be installed
# locally. Build the tools image once (cached after that):
#   docker build -f infra/docker/proto-tools.Dockerfile -t nexdesk-proto-tools infra/docker
set -euo pipefail
cd "$(dirname "$0")/.."

MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd):/work" -w /work nexdesk-proto-tools \
  -I proto \
  --prost_out=core/nexdesk-proto/src/generated \
  --go_out=backend/gen --go_opt=paths=source_relative \
  --go-grpc_out=backend/gen --go-grpc_opt=paths=source_relative \
  proto/nexdesk/v1/session.proto \
  proto/nexdesk/v1/rendezvous.proto \
  proto/nexdesk/v1/relay.proto

echo "Generated code written to core/nexdesk-proto/src/generated and backend/gen."
