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

**Phase 1 (Rust Core), Week 5 — Transport** partially done: `core/nexdesk-core/src/transport`

- `tcp.rs` — real TCP transport (`TcpConnection`/`TcpListener`) with length-prefixed framing, a max-frame-size guard against hostile length prefixes, tested against actual sockets (round-trip, oversized frame rejected, clean error on peer disconnect).
- `backoff.rs` — exponential reconnect backoff, capped, with reset.
- `keepalive.rs` — heartbeat ticker (`tokio::time::Interval` wrapper), tested with a paused clock.
- `mod.rs` — the `Connection` trait TCP (and now TLS) implements; QUIC (R-09, an 8-point item — "split if possible" per the planner's own scale) is deliberately **not** implemented yet, since it needs its own TLS/certificate setup and deserves to land as its own focused piece of work rather than being folded in here.
- `framing.rs` — length-prefixed message framing, shared by TCP and TLS so they can't drift on wire format.

**Phase 1 (Rust Core), Week 6 — Security** done: `core/nexdesk-core/src/crypto`. Key exchange (R-15) is entirely TLS 1.3's own ECDHE via `rustls` — nothing here implements cryptographic primitives, same "don't hand-roll what a vetted library does" principle as the codec decision.

- `tls.rs` — real TLS 1.3 over the existing TCP transport (`TlsConnection<S>` implements `Connection`, so callers can't tell it apart from plain TCP). Certificate verification **pins** the peer's exact cert rather than disabling verification (no CA in a P2P tool yet) — a real design decision, not a shortcut; production needs the pinned cert to come from a trusted channel (rendezvous/device identity, Phase 3). Tested against a real handshake (protocol version asserted as TLS 1.3) and a rejected mismatched-pin case.
- `credential_store.rs` — `CredentialStore` trait + in-memory impl for tests; a real desktop build needs an OS-backed implementation (Windows Credential Manager, macOS Keychain, libsecret) — that's Phase 4 platform work, not core-crate work.
- `replay.rs` — sliding-window nonce replay guard (the IPsec/DTLS anti-replay technique) for application-level messages, as defense in depth on top of TLS's own record-layer replay protection.
- **`tests/gate_g1.rs`** — the actual Gate G1 acceptance test: two real peers establish a TCP+TLS 1.3 session, exchange a `SessionHello` handshake and protocol messages, shut down cleanly, and reconnect — all five Gate G1 criteria in one end-to-end test, nothing mocked.

26 unit tests + the Gate G1 integration test, all passing locally, in Docker (Linux), and downstream in the napi addon. Protocol wire-up (frame reassembly/jitter/backpressure — R-19..R-23) and input encoding are next (Week 7), plus QUIC (R-09) whenever that gets picked up.
