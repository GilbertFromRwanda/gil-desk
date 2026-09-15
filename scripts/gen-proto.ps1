# Regenerate Rust + Go code from proto/nexdesk/v1/*.proto (planner task F-11).
# Runs protoc in a container so protoc/plugins don't need to be installed
# locally. Build the tools image once (cached after that):
#   docker build -f infra/docker/proto-tools.Dockerfile -t nexdesk-proto-tools infra/docker
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

docker run --rm -v "${PWD}:/work" -w /work nexdesk-proto-tools `
  -I proto `
  --prost_out=core/nexdesk-proto/src/generated `
  --go_out=backend/gen --go_opt=paths=source_relative `
  --go-grpc_out=backend/gen --go-grpc_opt=paths=source_relative `
  proto/nexdesk/v1/session.proto `
  proto/nexdesk/v1/rendezvous.proto `
  proto/nexdesk/v1/relay.proto

Write-Host "Generated code written to core/nexdesk-proto/src/generated and backend/gen." -ForegroundColor Green
