# NexDesk

Cross-platform remote desktop platform (Rust core, C++ codec, Go backend, Electron desktop client). See [NexDesk_Improved_Planner.md](NexDesk_Improved_Planner.md) for the full architecture, phased roadmap, and decision log — this README only covers running what exists today.

## Layout

```text
core/      Rust workspace — session/transport/crypto/protocol/input/rendezvous + FFI boundary
codec/     C++ capture/codec library (wraps libx264/FFmpeg — see planner Section 6)
backend/   Go control plane — auth, rendezvous, relay, registry
desktop/   Electron + React + TypeScript desktop client
proto/     Protobuf network contracts
infra/     Docker Compose dev stack, k8s, terraform
```

## Prerequisites

- Rust (`cargo`) — for `core/`
- Go 1.25+ — for `backend/`
- Docker — for `codec/` (no local CMake/C++ toolchain required) and the dev stack
- Node.js 20+ — for `desktop/`

## Build & test what exists

```bash
# Rust core + FFI + generated protobuf types
cd core && cargo test --workspace

# Go backend (+ generated protobuf types under backend/gen)
cd backend && go build ./... && go test ./...

# C++ codec (builds in a container — no local CMake needed)
docker build -t nexdesk-codec-build codec

# Rust <-> C++ FFI, proven for real (compiles+links+calls into the C++ codec —
# off by default locally since it needs a C++ compiler; verify via Docker)
docker build -f core/Dockerfile -t nexdesk-core-ffi-verify .

# Rust <-> Node (napi-rs) FFI, proven for real: builds a native addon and
# calls into it from a plain Node script, no npm install required
cd desktop/native && ./build.sh && node smoke-test.js   # or build.ps1 on Windows

# Desktop app itself (scaffolded, not yet installed/built)
cd desktop && npm install && npm run build
```

## Regenerating protobuf code

`proto/nexdesk/v1/*.proto` is the source of truth; generated code lives in `core/nexdesk-proto/src/generated/` and `backend/gen/` and is checked in (regeneration needs Docker, not every dev machine). After editing a `.proto` file:

```bash
docker build -f infra/docker/proto-tools.Dockerfile -t nexdesk-proto-tools infra/docker  # once, or after Dockerfile changes
./scripts/gen-proto.sh   # or scripts/gen-proto.ps1 on Windows
```

## Dev stack (Postgres, Redis, backend, Prometheus, Grafana)

```bash
docker compose -f infra/docker/docker-compose.yml up --build
```

Host ports are intentionally non-standard (this machine runs several other projects' dev stacks): backend `18080`, Postgres `55432`, Redis `56379`, Prometheus `19090`, Grafana `13000`. See the comments in [infra/docker/docker-compose.yml](infra/docker/docker-compose.yml) before changing them back to standard ports.

## Status

**Phase 0 (Foundation)** functionally complete: workspaces compile and test, protobuf codegen is wired end-to-end (Rust encode/decode round-trip verified, Go types generated and building), and both cross-language boundaries are proven to actually link and call — not just build in isolation — Rust↔C++ (Docker-verified, Gate G0's "Rust can call C++") and Rust↔Node/napi-rs (`desktop/native`, F-18/F-19). CI (`.github/workflows/ci.yml`) is pushed to GitHub but currently **disabled** (`gh workflow enable CI` to turn it back on).

**Phase 1 (Rust Core), Week 4 — Session engine** done: `core/nexdesk-core/src`

- `session/` — a real state machine (`Session`/`SessionState`/`SessionEvent`), not a bare enum: enforces the Gate G1 lifecycle (connect → handshake → established → disconnect → closed → reconnect), rejects illegal transitions without mutating state, 7 tests covering the happy path, failure paths, and reconnect.
- `config.rs` — env-var config loader (`NEXDESK_*`) with validated parsing and defaults.
- `error.rs` — crate-wide `NexError` (via `thiserror`).
- `shutdown.rs` — coordinated cancellation (`tokio_util::CancellationToken`-backed `Shutdown` handle), tested with a real spawned task observing the signal.

Transport, crypto, protocol wire-up, and input encoding (R-07 onward) are still unimplemented stubs — that's Week 5+.
