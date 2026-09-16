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
- Go 1.26+ — for `backend/` (bumped from 1.25 when `golang.org/x/sys`, pulled in transitively by `testcontainers-go`/`grpc`/`go-redis`, raised its own minimum)
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
- `mod.rs` — the `Connection` trait TCP (and now TLS, and now QUIC — see below) implements.
- `framing.rs` — length-prefixed message framing, shared by TCP and TLS so they can't drift on wire format.

**Phase 1 (Rust Core), Week 6 — Security** done: `core/nexdesk-core/src/crypto`. Key exchange (R-15) is entirely TLS 1.3's own ECDHE via `rustls` — nothing here implements cryptographic primitives, same "don't hand-roll what a vetted library does" principle as the codec decision.

- `tls.rs` — real TLS 1.3 over the existing TCP transport (`TlsConnection<S>` implements `Connection`, so callers can't tell it apart from plain TCP). Certificate verification **pins** the peer's exact cert rather than disabling verification (no CA in a P2P tool yet) — a real design decision, not a shortcut; production needs the pinned cert to come from a trusted channel (rendezvous/device identity, Phase 3). Tested against a real handshake (protocol version asserted as TLS 1.3) and a rejected mismatched-pin case.
- `credential_store.rs` — `CredentialStore` trait + in-memory impl for tests; a real desktop build needs an OS-backed implementation (Windows Credential Manager, macOS Keychain, libsecret) — that's Phase 4 platform work, not core-crate work.
- `replay.rs` — sliding-window nonce replay guard (the IPsec/DTLS anti-replay technique) for application-level messages, as defense in depth on top of TLS's own record-layer replay protection.
- **`tests/gate_g1.rs`** — the actual Gate G1 acceptance test: two real peers establish a TCP+TLS 1.3 session, exchange a `SessionHello` handshake and protocol messages, shut down cleanly, and reconnect — all five Gate G1 criteria in one end-to-end test, nothing mocked.

**Phase 1 (Rust Core), Week 7 — Protocol/media plumbing** done: `core/nexdesk-core/src/protocol` and `src/input`

- `protocol/codec.rs` — generic protobuf encode/decode helpers over `prost`.
- `protocol/reassembly.rs` — `FrameReassembler`: buffers out-of-order chunks per `frame_id` until complete, bounded (oldest incomplete frame evicted first) so this can't become the unbounded buffer the planner's soak tests exist to catch.
- `protocol/jitter.rs` — `JitterBuffer`: releases frames in sequence order, but skips a persistent gap once `max_wait` passes rather than blocking forever on one late frame. Deterministically testable (takes an explicit `now`/`arrived_at` instead of reading the wall clock).
- `protocol/backpressure.rs` — `FrameQueue`: bounded, drop-oldest-on-overflow (favors latency over completeness for real-time video), tracks a `dropped_count()` for the planner's `frames_dropped` metric.
- `input/mod.rs` — hand-rolled compact binary encoding for `InputEvent` (not protobuf — these are small, extremely high-frequency messages where general-purpose framing overhead is real cost). Decoding untrusted bytes is bounds-checked throughout and proven not to panic on malformed/truncated input.

**Phase 1 (Rust Core), Week 8 — Vertical slice** done: `core/nexdesk-core/src/bin/{host,client}.rs`. Real, runnable binaries — the point of this week is that Gate G1 is satisfied by actual compiled artifacts, not just library tests.

- `host.rs` — binds, accepts connections, completes the TLS+SessionHello handshake, heartbeats, and now **persists its dev identity across restarts** (`<cert>.key` alongside the cert file) — the first run of this generated a fresh cert every restart, which made every previously-connected client's pinned cert go stale; that's now fixed, since a real device's identity should be stable.
- `client.rs` — connects, handshakes, heartbeats, and **reconnects with exponential backoff** (reusing `transport::backoff::Backoff`) instead of exiting when the connection drops.
- `tests/cli_vertical_slice.rs` — spawns the actual compiled binaries as separate OS processes (`env!("CARGO_BIN_EXE_*")`, not library calls) and drives them through connect → secure handshake → heartbeat exchange → kill the host → confirm the client retries → restart the host → confirm the client reconnects → clean shutdown. This is what actually exercises R-26/R-27/R-28, on top of what `tests/gate_g1.rs` already covers at the library level.
- Manually verified end-to-end in two real terminals first (documented as a process, not just claimed): connect, live heartbeats both directions, kill `-9` the host mid-session, watch the client back off (200ms → 400ms → 800ms → ...), restart the host, watch the client reconnect. That's what caught the stale-identity-on-restart bug above — the automated test came second, once the manual run showed what to assert.

Try it yourself:

```bash
cd core/nexdesk-core
cargo run --bin host -- --bind 127.0.0.1:7000 --device-id my-host
# in a second terminal:
cargo run --bin client -- --server 127.0.0.1:7000 --cert nexdesk_host_cert.der --device-id my-client
```

47 unit tests + 2 integration tests (`gate_g1`, `cli_vertical_slice`), all passing locally, in Docker (Linux), and downstream in the napi addon, zero clippy warnings. **This closes Phase 1 through Week 8**, i.e. Gate G1 and milestone M1. (QUIC, R-09, was deferred from this week — see the dedicated section further down; it's since been implemented.)

**Phase 2 (C++ Capture + Codec), codec integration** (C-06/C-09/C-10 — not capture yet, see below) done: `codec/src` + `core/ffi/src/codec.rs`. Real `libx264` encode and `libavcodec`/FFmpeg decode — as decided in planner Section 2, nothing here implements H.264 itself.

- `codec/include/nexdesk/codec.h` — the C ABI: opaque `nd_encoder_t`/`nd_decoder_t` handles, explicit ownership (every `_create` has a `_destroy`, every "owned" buffer is freed via `nd_buffer_free`), operating on planar I420 frames. ABI version bumped 1 → 2 for this addition (both `nd_codec_abi_version()` and `core/ffi`'s `nd_ffi_abi_version()`, kept in lockstep and asserted equal by a test).
- `codec/src/encoder.cpp` — wraps `x264_encoder_encode`, `baseline` profile, `zerolatency` preset (real-time, not throughput-optimized) via `CMakeLists.txt`'s `pkg_check_modules(x264)`.
- `codec/src/decoder.cpp` — wraps `avcodec_send_packet`/`avcodec_receive_frame` (`libavcodec`'s H.264 decoder) via `pkg_check_modules(libavcodec libavutil)`.
- `codec/tests/test_codec.cpp` — a synthetic I420 gradient test frame (no real capture needed) proves a real encode→decode round-trip: dimensions and buffer size match on the far side. Also covers invalid-dimension rejection and garbage input not crashing the decoder.
- `core/ffi/src/codec.rs` — safe Rust `Encoder`/`Decoder` wrappers (RAII `Drop` for the C handles) over the same C ABI, feature-gated behind `real-codec-link` (same reasoning as the FFI proof: needs a C++ compiler + libx264/libavcodec dev packages, not guaranteed on every dev machine — verify via `core/Dockerfile`, updated to install `libx264-dev libavcodec-dev libavutil-dev`). Its own round-trip test proves the *entire* path: Rust generates a synthetic frame → C ABI → libx264 encode → libavcodec decode → C ABI → Rust gets back a valid decoded frame.

Capture (C-01..C-05: the actual screen-grab, Windows/Linux/macOS-specific) is **not** implemented yet — it needs a real display, which isn't available in this dev/CI environment (headless Docker, no X server), so it couldn't be verified here the way everything else in this repo has been. Codec integration was picked first specifically because it's fully testable without one (synthetic frames stand in for a captured screen).

**Phase 3 (Go Control Plane), backend foundation + rendezvous** done, including session authorization: `backend/internal/{registry,rendezvous}`, `backend/cmd/server`. Real Postgres + Redis + gRPC, not stubs — this closes out G-01 through G-12.

- `internal/registry/migrate.go` — a small hand-rolled migration runner (deliberately not a library: this is a dozen straightforward lines, not complex/security-sensitive logic like the crypto/codec work elsewhere in this repo) — applies `migrations/*.sql` in order, each in its own transaction, tracked in a `schema_migrations` table. `0001_devices.sql` creates `devices`; `0002_device_authorizations.sql` creates the authorization ACL (see below).
- `internal/registry/registry.go` — `Store`, backed by `pgxpool`: durable device identity (`UpsertDevice`/`GetDevice`, idempotent), plus `AuthorizeDevice`/`IsAuthorized` — a simple ACL of which device IDs may request a session with which (there's no account/pairing UI yet, so this is the only way to populate it for now).
- `internal/registry/presence.go` — `Presence`, backed by Redis TTL keys: short-lived endpoint/online tracking (planner G-11), deliberately *not* in Postgres — this is high-churn, disposable data, not identity.
- `internal/rendezvous/token.go` — `TokenIssuer` (planner G-12): short-lived signed session-authorization tokens, built on the standard library's `crypto/hmac`+`crypto/sha256` (not a hand-rolled signature scheme — same "don't invent your own crypto" rule as `core/crypto/tls.rs`). Scoped to this package rather than `internal/auth`, since G-12 belongs to the Rendezvous week, not the later G-13..G-19 account/JWT system `internal/auth` is reserved for.
- `proto/nexdesk/v1/rendezvous.proto` — a real gRPC service (`RendezvousService`: `RegisterDevice`, `Heartbeat`, `LookupPeer`, `AuthorizeDevice`, `RequestSession`), not just message definitions; regenerated via the existing `scripts/gen-proto.sh` pipeline.
- `internal/rendezvous/rendezvous.go` — the gRPC service implementation, wiring `Store` + `Presence` + `TokenIssuer` together. `RequestSession` checks the ACL and, only if authorized, issues a signed token — denial is an explicit `authorized: false`, not silence.
- `cmd/server/main.go` — connects to Postgres/Redis, runs migrations on startup, serves gRPC (`:9090` by default) alongside the HTTP health server, structured JSON logging (`log/slog`, G-07), graceful shutdown on SIGINT/SIGTERM. `/readyz` actually pings Postgres and Redis. Session-signing key comes from `NEXDESK_SESSION_SIGNING_KEY`, or an ephemeral per-process random key with a logged warning if unset (fine given the short 60s token TTL, but a production deployment should set it explicitly so a rolling restart doesn't strand in-flight authorizations).
- `internal/rendezvous/token_test.go` — unit tests: issue/verify round-trip, tampered token rejected, wrong signing key rejected, expired token rejected, malformed input rejected.
- `internal/rendezvous/rendezvous_test.go` — a real integration test against actual Postgres and Redis containers (`testcontainers-go`, not mocks): register → lookup, heartbeat → online status, unregistered-device lookup, re-registration, and the full authorization flow (denied → `AuthorizeDevice` → granted with a token that verifies). Needs a real Docker daemon, so it's skipped with `-short` on CI's Windows/macOS runners (not guaranteed to have one) and run in full on Linux. Hit a real flaky race here (Postgres's one-time restart during first-run init sometimes takes longer than expected, worse when other packages' containers are starting concurrently) — fixed by widening the connection-retry budget rather than assuming best-case timing; confirmed reliable across several consecutive full-suite runs afterward.
- Manually verified against the actual dev stack too (`infra/docker/docker-compose.yml`), not just the test containers: ran the real server binary, confirmed `/healthz`/`/readyz`, and checked the tables directly in Postgres via `psql`.

**Phase 3 (Go Control Plane), authentication foundation** done: `backend/internal/auth`, HTTP endpoints on `cmd/server`. Real accounts, JWTs, and refresh-token rotation — this covers G-13/G-14/G-15. **Not done**: 2FA/TOTP (G-16), rate limiting (G-18), audit events (G-19).

- `internal/auth/account.go` — `AccountStore`: `bcrypt` password hashing (`golang.org/x/crypto`, not hand-rolled), `VerifyPassword` returns the same error for "no such email" and "wrong password" so it can't be used to enumerate registered accounts.
- `internal/auth/token.go` — `TokenIssuer`: JWT access tokens via `golang-jwt/jwt` (a vetted library, not a hand-rolled token format — distinct from `rendezvous.TokenIssuer`'s narrower non-JWT session token). Verification explicitly rejects non-HMAC signing methods, guarding against the classic JWT "alg confusion" attack.
- `internal/auth/refresh.go` — `RefreshStore`: refresh tokens are stored only as a SHA-256 hash (a DB leak alone shouldn't yield usable tokens) and are single-use — `Rotate` atomically revokes the presented token and issues a replacement in one transaction, so a stolen-and-reused token fails on its second use.
- `internal/api/api.go` — `POST /auth/register`, `/auth/login`, `/auth/refresh` (HTTP+JSON, not gRPC — this is a user-facing flow, unlike the Rust↔Go device rendezvous boundary which is gRPC per the planner's architecture decision).
- Real dependency-graph friction, handled rather than papered over: adding these libraries pulled in a newer transitive `golang.org/x/sys` (via `testcontainers-go`/`grpc`/`go-redis`) that requires Go 1.26. Rather than leave `go.mod` silently ahead of what CI/Dockerfiles declared, bumped `go 1.25.0` → `1.26.0` everywhere that needs to match: `.github/workflows/ci.yml`'s `go-version`, `backend/Dockerfile`'s base image (also fixed a latent bug there — it never copied `go.sum` before `go mod download`). `infra/docker/proto-tools.Dockerfile`'s Go stage compiles unrelated standalone tool binaries from their own `go.mod` and didn't need touching.
- Unit tests (`token_test.go`): JWT issue/verify, wrong signing key rejected, expired token rejected, malformed input rejected. Integration test (`auth_test.go`, real Postgres via `testcontainers-go`): create/verify account, duplicate email rejected, wrong password and unknown email both reject identically, refresh rotation is single-use (reusing an already-rotated token fails, the new one still works).
- Manually verified end-to-end against the real dev stack too: registered a user via `curl`, hit the duplicate-email and wrong-password cases, logged in, rotated a refresh token, confirmed reusing the old one 401s, and checked the `users`/`refresh_tokens` rows directly in Postgres.

**Phase 3 (Go Control Plane), G-17 — device authorization tied to real identity** done, closing a real gap: until now, `RegisterDevice`/`Heartbeat`/`AuthorizeDevice`/`RequestSession` trusted whatever `device_id` a caller put in the request — anyone could claim to be any device. They now require a JWT access token (from G-14, sent as gRPC `authorization: Bearer <token>` metadata — no proto changes needed, metadata isn't part of the message schema) and check the caller's user actually owns the device_id(s) being acted on. `LookupPeer` deliberately stays open — it's read-only, doesn't reveal ownership, and requiring auth would block discovering a peer before pairing exists.

- `migrations/0005_devices_owner.sql` adds `devices.owner_user_id`. `registry.Store.UpsertDevice` now takes the owner and uses a conditional `ON CONFLICT ... WHERE devices.owner_user_id = EXCLUDED.owner_user_id` — an atomic, race-free way to detect "someone else already owns this device_id" without a separate check-then-write. `IsDeviceOwner` backs the per-RPC ownership check.
- `rendezvous.Service.authenticate` extracts and verifies the Bearer token; `requireOwner` is the shared ownership check every mutating RPC calls before acting.
- The integration test now proves the actual attack scenarios this closes, not just the happy path: a second user can't re-register (hijack) someone else's device_id, can't send heartbeats for it, can't authorize access to it, and can't impersonate it as a session requester — all four assert `PermissionDenied`. A separate case covers missing/malformed/garbage tokens asserting `Unauthenticated`.

**Phase 3 (Go Control Plane), G-18 — login rate limiting** done: `internal/auth/ratelimit.go`. **Not done**: 2FA/TOTP (G-16).

- `RateLimiter`: a fixed-window counter on Redis `INCR`+`EXPIRE` — the standard pattern for this, not something that needs a library. Applied to `/auth/login`, keyed by email: 5 attempts per 15 minutes, reset on a successful login so a legitimate user's earlier typos don't count against them. Not yet applied to `/auth/register` (a different threat model — account-creation spam, not credential guessing) or keyed by IP (no reverse-proxy topology exists yet to trust a client-supplied IP header from).
- Caught and fixed a real latent flaky test while adding this: `rendezvous`'s `TestTokenRejectsTampering` flipped a token's last character to a fixed `'x'` to simulate tampering, but ~1/64 of the time (base64url alphabet) the original character already *was* `'x'`, silently turning the "tampering" into a no-op and occasionally failing the test for real. Confirmed the fix (always flip to something guaranteed different) holds across 200 consecutive runs.
- Unit tests (`auth_test.go`, real Redis via `testcontainers-go`): allows up to the limit then blocks, `Reset` restores access, different keys (accounts) are independent.
- Manually verified end-to-end against the real dev stack: 5 wrong-password attempts got normal 401s, the 6th got 429, the *correct* password was still rejected with 429 while blocked, a different account was unaffected, and the Redis key/TTL matched expectations via `redis-cli`.

**Phase 3 (Go Control Plane), G-19 — audit events** done: `internal/audit`.

- `internal/audit/audit.go` — `Logger`, backed by Postgres (`audit_events`), not just log lines: an audit trail needs to answer "who did what, when" durably, after the fact — grepping logs doesn't reliably give you that. Deliberately **no FK** from `audit_events.user_id` to `users(id)` — an audit log needs to survive independently of the lifecycle of what it references (a user being deleted, e.g. for a data-deletion request, shouldn't retroactively block or corrupt audit history). `userID` is stored as `NULL`, not a fake sentinel, when an event has no associated user (e.g. a failed login against an unknown email).
- Wired into the actual decision points, not bolted on separately: `api.AuthHandlers` logs `user.registered`, `login.succeeded`, `login.failed`, and `login.rate_limited`; `rendezvous.Service` logs `device.registered` and `device.authorized`. Logging is best-effort — a logging failure is a warning (`slog.Warn`), never something that blocks a legitimate request.
- Integration test (`audit_test.go`, real Postgres via `testcontainers-go`): logs with a user, logs with no user as `NULL`, and `Recent` returns newest-first respecting its limit.
- Manually verified end-to-end: registered a user, logged in successfully, failed 5 logins, then got rate-limited, and confirmed all 8 events landed in `audit_events` with exactly the right `has_user` flag on each.

**Phase 3 (Go Control Plane), G-16 — 2FA/TOTP** done. **This closes G-01 through G-19 — Phase 3's Authentication section is now fully done.**

- `internal/auth/totp.go` — a thin wrapper over `pquerna/otp` (a vetted RFC 6238 implementation, not hand-rolled), generating/validating standard `otpauth://` secrets and codes any authenticator app understands.
- `internal/auth/pending_login.go` — `PendingLoginIssuer`: a short-lived (5 min) signed token proving "this caller already gave the right password and just needs to complete the TOTP challenge" — built on `crypto/hmac`+`crypto/sha256` like `rendezvous.TokenIssuer`, but deliberately its own distinct type rather than a shared one, so it can never be confused with a real JWT access token or a device session token.
- `migrations/0007_users_totp.sql` adds `users.totp_secret`/`totp_enabled`. **A real gap, stated rather than hidden**: the secret is stored raw, not encrypted at rest — this system has no key-management infrastructure yet (session/JWT signing keys are also just plain env vars), so there's nothing to meaningfully encrypt it *with* yet. Production needs KMS-backed envelope encryption before this is acceptable for real accounts.
- Enrollment is verify-before-enable (`POST /auth/2fa/enroll` generates a pending secret; `POST /auth/2fa/verify` only flips `totp_enabled` to true once a real code from that secret is presented) — so a broken authenticator-app setup can't lock someone out of an account they haven't actually secured yet.
- `POST /auth/login` now branches: if the account has 2FA enabled, it returns `{two_factor_required: true, pending_token}` instead of tokens; `POST /auth/login/2fa` completes the challenge with `{pending_token, code}` and only then issues real tokens.
- Unit tests: `pending_login_test.go` (issue/verify, tampering, wrong key, expiry, malformed input — same shape as `rendezvous`'s token tests, tampering test written correctly from the start this time) and `totp_test.go` (generate/validate round-trip, wrong code rejected, a code for a different secret rejected).
- Integration test (`api/api_test.go`, real Postgres+Redis via `testcontainers-go`) drives the actual HTTP handlers end to end, not just the logic underneath: register → login (no 2FA yet, succeeds immediately) → enroll → verify with wrong code (rejected, not enabled) → verify with right code (enabled) → login (now challenged) → complete challenge with wrong code (rejected) → complete with right code (real tokens issued).
- Manually verified against the real dev stack too, with real codes computed from the actual secret (a throwaway RFC 6238 script, not part of the shipped codebase — the shipped system uses the vetted library): the full enroll → verify → challenged-login → complete flow, and confirmed the `user.2fa_enabled`/`login.2fa_failed`/`login.succeeded` audit events and `users.totp_enabled` state directly via `psql`.

**Phase 1 (Rust Core), Week 5 — QUIC transport (R-09)** done, closing the one item deferred from Week 5 back when TCP/TLS/backoff/keepalive landed: `core/nexdesk-core/src/transport/quic.rs`. Built on `quinn`, whose QUIC handshake runs directly on the same `rustls::ClientConfig`/`ServerConfig` the TLS transport already produces (`crypto::tls::server_config`/`client_config_pinned_to`) — no new certificate or verification logic needed, only wiring; `QuicConnection` implements the same `Connection` trait as TCP and TLS, over a single bidirectional QUIC stream, reusing the same `framing` module.

- Two real quinn lifetime bugs hit and fixed while building this, both the same shape (a handle silently keeping a connection alive was dropped too early) and worth naming because they're easy to reintroduce: (1) `accept()`/`connect()` each had a local `quinn::Connection` that was dropped the moment the function returned, after only extracting its streams — dropping the last `Connection` handle closes the connection even with active streams, so it now stays in `QuicConnection` as a kept-alive field. (2) The client's `quinn::Endpoint` was purely local to `connect()` and dropped for the same reason — kept alive the same way (the server side doesn't have this problem, since `QuicListener` already owns its endpoint for as long as the listener itself lives).
- A third, more fundamental difference from TCP surfaced in testing and is now called out in the module's own test rather than hidden: QUIC's send buffer lives entirely in userspace, so a write can return `Ok` before it's actually been transmitted — unlike TCP, where the kernel already has the bytes by the time `write_all` returns. Dropping a `QuicConnection` immediately after its last write can silently lose that write. The fix isn't a workaround in this module (there's nothing to patch — this is correct, expected QUIC behavior); it's a usage rule any real caller needs to follow: don't tear down a QUIC connection immediately after a write you need delivered. Confirmed the round-trip test is now deterministic, not a lucky timing window, across 30 consecutive runs.
- Also needed ALPN configured on both sides' rustls configs (`quic.rs` clones the shared TLS config and adds one) — required for QUIC specifically; the plain TLS-over-TCP transport doesn't need it, so it isn't set on the shared `tls::server_config`/`client_config_pinned_to` functions themselves.
- Tests mirror `crypto::tls`'s: a real handshake + round-trip, and a rejected mismatched-pin case. 44 unit tests total in `nexdesk-core` now, verified locally, in the Docker FFI build (Linux), and downstream in the napi addon.

**Phase 4 (Electron Desktop MVP), first real login (E-06 Login, E-08 Session status)** done: `desktop/main`, `desktop/preload`, `desktop/renderer/src`. This is the first time the Electron app itself was actually launched in this repo's history (everything in `desktop/` before now was scaffolding plus the separately-verified `native/` napi bridge) — and the first genuine UI-to-backend integration in the whole project: a real login form driving the real HTTP API built in Phase 3.

- Fixed a real gap in the build pipeline while getting the app to launch at all: `package.json`'s `build` script compiled the renderer and main process but never compiled `preload/index.ts` to JS — `main/index.ts` pointed at a `preload/index.js` that no build step ever produced. Added `tsconfig.preload.json` (its own CommonJS project, like `tsconfig.main.json`) and a `build:preload` step.
- `main/authClient.ts` — the only module that actually calls the backend (`fetch` against `POST /auth/register`, `/login`, `/login/2fa`), deliberately framework-free (no Electron imports) so it's just as callable from a plain Node script as from the main process. Wired into `main/index.ts` via `ipcMain.handle("auth:register"/"auth:login"/"auth:loginTwoFactor", ...)`.
- `preload/index.ts` — extended the existing minimal bridge with a narrow `nexdesk.auth.{register,login,loginTwoFactor}` surface over `ipcRenderer.invoke`. The renderer still never gets a raw `fetch`-to-backend or `ipcRenderer` itself — same least-privilege posture `contextIsolation`/`nodeIntegration: false` already established in `main/index.ts`.
- `renderer/src/App.tsx` — a real login/register form plus the 2FA challenge screen, wired to the actual state shape `api.go` returns (`two_factor_required`/`pending_token` branches to the TOTP prompt, `access_token` branches to a logged-in view). **Stated gap, not hidden**: tokens live only in React state, so they don't survive an app restart — persisting them needs an OS-backed secure store (Electron's `safeStorage`, keychain/DPAPI-backed) behind its own IPC call, not built yet.
- Verified as an actual end-to-end run, not just a build: started the real dev-stack Postgres/Redis, ran the real `cmd/server` binary, launched the real Electron app (`npm start`) pointed at it, and drove the actual rendered window — typed a real email/password into the real form fields and submitted — via OS-level keystroke injection (`SendKeys`/`SetForegroundWindow`) rather than a mocked renderer, confirmed by screenshot showing "Logged in as desktop-test@example.com" with a real JWT, and cross-checked against Postgres directly (`SELECT * FROM audit_events`) showing the matching real `login.succeeded` row at the same timestamp — the same "verify against real infrastructure" standard as every earlier phase, just through a GUI this time instead of `curl`.
- Hit and fixed one environment-specific issue along the way: this shell had `ELECTRON_RUN_AS_NODE=1` set, which makes `electron.exe` run as plain Node (so `require("electron")` returns a path string instead of the real API, crashing on the first `ipcMain` access) — unrelated to the app itself, just how this particular dev shell is configured; unset it to actually launch the app.
- Not done yet: token persistence (above), and real screen capture (C-01..C-05) remains blocked on the same thing it's been blocked on since Phase 2 — no real display in this dev/CI environment for the actual capture path, and no Windows C++ toolchain installed locally for building `codec/` outside Docker's Linux containers. Both need an explicit environment change (installing a toolchain, or switching Docker to Windows containers) that hasn't been made without asking.
