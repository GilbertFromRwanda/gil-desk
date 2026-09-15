# NexDesk — Engineering Master Planner
## Remote Desktop / Remote Support Platform

> **Goal:** Build a secure, cross-platform remote desktop platform with a Rust networking/core engine, C++ media codec layer, Go control-plane/backend, an Electron desktop client, and a web client. Mobile clients are explicitly out of scope (see Section 30's changelog).
>
> **Planning principle:** Build the smallest end-to-end vertical slice first, then harden and scale it. Do not wait until Week 24 to discover that transport, FFI, rendering, NAT traversal, or security assumptions are wrong.

---

## Contents

**§0** [Executive Plan](#0-executive-plan) · **§1** [Architecture](#1-architecture) · **§2** [Critical Architecture Decisions](#2-critical-architecture-decisions) · **§3** [Repository Structure](#3-repository-structure)
**§4** [Phase 0 — Foundation](#4-phase-0--foundation) · **§5** [Phase 1 — Rust Core](#5-phase-1--rust-core) · **§6** [Phase 2 — C++ Capture + Codec](#6-phase-2--c-capture--codec) · **§7** [Phase 3 — Go Control Plane](#7-phase-3--go-control-plane)
**§8** [Phase 4 — Electron Desktop MVP](#8-phase-4--electron-desktop-mvp) · **§9** [Phase 5 — Production Features](#9-phase-5--production-features) · **§10** [Phase 6 — Web Client](#10-phase-6--web-client) · **§11** [Phase 8 — Security, Reliability & Launch](#11-phase-8--security-reliability--launch)
**§12** [Security Workstream](#12-security-workstream) · **§13** [Observability](#13-observability) · **§14** [Performance Budgets](#14-performance-budgets) · **§15** [Testing Strategy](#15-testing-strategy)
**§16** [Load Testing](#16-load-testing) · **§17** [Failure Engineering](#17-failure-engineering) · **§18** [CI/CD Pipeline](#18-cicd-pipeline) · **§19** [Infrastructure](#19-infrastructure)
**§20** [FFI Governance](#20-ffi-governance) · **§21** [FFI Contract Example](#21-ffi-contract-example) · **§22** [Protocol Versioning](#22-protocol-versioning) · **§23** [Weekly Engineering Ritual](#23-weekly-engineering-ritual)
**§24** [Risk Register](#24-risk-register) · **§25** [MVP Scope Freeze](#25-mvp-scope-freeze) · **§26** [Critical Path](#26-critical-path) · **§27** [Milestone Acceptance Matrix](#27-milestone-acceptance-matrix)
**§28** [Final Release Checklist](#28-final-release-checklist) · **§29** [Recommended Execution Order](#29-recommended-execution-order) · **§30** [The Most Important Planner Changes](#30-the-most-important-planner-changes) · **§31** [Success Definition](#31-success-definition)

---

# 0. Executive Plan

## Assumptions & team composition

The week numbers in this planner are **not** absolute — they are only correct for a specific team shape. State it explicitly and revisit it whenever staffing changes:

| Assumption | Default used here | If this changes |
|---|---|---|
| Team size | ~8–12 engineers running Tracks A–E in [Section 29](#29-recommended-execution-order) concurrently | Fewer engineers → tracks that overlap on the calendar must run sequentially instead, and every date below shifts out |
| Velocity | ~8 pts/engineer/week sustained (lower than raw capacity to absorb meetings, review, on-call) | Junior-heavy or part-time team → lower velocity, extend weeks proportionally |
| Track independence | Tracks A–E are assumed to have their own dedicated engineer(s); a track with zero dedicated headcount does not progress | Shared/rotating staff across tracks → treat as one combined track and sum its points |

**Before treating any week's table as a commitment:** sum that week's story points and divide by the number of engineers actually dedicated to that track. If the result is above ~8–10 pts/engineer, either the week is overloaded or it silently assumes multiple engineers on that track — make that explicit rather than discovering it mid-sprint.

## Product milestones

| Milestone | Target | Outcome |
|---|---:|---|
| M0 | Week 3 | Reproducible cross-platform development/build pipeline |
| M1 | Week 8 | Secure headless desktop-to-desktop prototype |
| M2 | Week 12 | Real screen + input over LAN |
| M3 | Week 18 | Internet connectivity + NAT traversal + relay |
| M4 | Week 24 | Installable desktop MVP |
| M5 | Week 32 | Production desktop release candidate |
| M6 | Week 38 | Browser client |
| M7 | Week 46 | Security-hardened public v1.0 |

## Priority

- `[P0]` Blocker / security / critical path
- `[P1]` Required for milestone
- `[P2]` Important improvement
- `[P3]` Optional

## Story points

- `1` = trivial
- `2–3` = small
- `5` = medium
- `8` = large
- `13` = very large; split if possible

## Definition of Done

A task is not complete until applicable items are satisfied:

- [ ] Code implemented
- [ ] Unit tests added
- [ ] Integration test added when crossing a component boundary
- [ ] Error handling implemented
- [ ] Logs/metrics added for operationally important paths
- [ ] Security implications reviewed
- [ ] CI passes on supported platforms
- [ ] Documentation updated
- [ ] No known P0/P1 defect remains

---

# 1. Architecture

```text
                           ┌───────────────────────┐
                           │       Clients         │
                           │                       │
                           │    Electron │ Web     │
                           └──────────┬────────────┘
                                      │
                                      ▼
                           ┌───────────────────────┐
                           │     Rust Core         │
                           │                       │
                           │ Session / Transport   │
                           │ Crypto / Input        │
                           │ P2P / Relay client    │
                           └───────┬───────┬───────┘
                                   │       │
                         C ABI / FFI│       │gRPC/HTTPS
                                   ▼       ▼
                         ┌────────────┐  ┌─────────────┐
                         │ C++ Codec  │  │ Go Backend  │
                         │ Capture    │  │ Rendezvous  │
                         │ Encode     │  │ Relay       │
                         │ Decode     │  │ Auth        │
                         └────────────┘  │ Registry    │
                                         └──────┬──────┘
                                                │
                              ┌─────────────────┼────────────────┐
                              ▼                 ▼                ▼
                         PostgreSQL           Redis        Object Storage
```

## Component ownership

| Component | Language | Responsibility |
|---|---|---|
| `core` | Rust | Session engine, networking, crypto, protocol, input |
| `codec` | C++ | Capture, pixel conversion, encoding/decoding, HW acceleration |
| `backend` | Go | Auth, rendezvous, relay, device/session registry |
| `desktop` | Electron + React/TS | Desktop UX, IPC, installation/update |
| `web` | TypeScript/Svelte | Browser client |
| `proto` | Protobuf | Network contracts |
| `infra` | Docker/K8s/Terraform | Deployment and operations |

---

# 2. Critical Architecture Decisions

These decisions must be frozen before large implementation work.

| Decision | Choice | Reason |
|---|---|---|
| Core | Rust | Safety + async networking + portable native core |
| Codec | C++ | Mature multimedia/native acceleration ecosystem |
| Video codec implementation | Wrap libx264 (encode) + FFmpeg/libavcodec or OpenH264 (decode), plus hardware encoders | Writing a competitive H.264 encoder/decoder in-house is a multi-year, dedicated-team effort (see `x264`/`FFmpeg` project history) — not a schedulable task. Every shipping remote-desktop product wraps existing codec libraries instead of writing its own |
| Desktop shell | Electron | Fast cross-platform product development |
| Backend | Go | Concurrency, networking, operational simplicity |
| Local native boundary | C ABI | Stable ABI and language independence |
| Rust ↔ Electron | napi-rs | Native Node integration |
| Network contract | Protobuf | Versioned, language-neutral contract |
| Rust ↔ Go | gRPC | Strong typed RPC and streaming support |
| Primary media transport | QUIC | Low latency + multiplexing |
| Fallback | TCP/TLS or WebSocket where required | Connectivity |
| NAT traversal | ICE/STUN/TURN-style architecture | Internet connectivity |
| Database | PostgreSQL | Durable control-plane data |
| Cache/presence | Redis | Short-lived registry/session state |
| Desktop rendering | GPU-oriented path | Avoid unnecessary JS pixel copies |
| CI | GitHub Actions | Cross-platform automation |
| Packaging | Native installers | User-friendly deployment |

---

# 3. Repository Structure

```text
nexdesk/
├── core/
│   ├── session/
│   ├── transport/
│   ├── crypto/
│   ├── protocol/
│   ├── input/
│   ├── rendezvous/
│   ├── ffi/
│   └── tests/
├── codec/
│   ├── include/
│   ├── src/
│   ├── capture/
│   ├── encoder/
│   ├── decoder/
│   ├── platform/
│   └── tests/
├── backend/
│   ├── cmd/
│   ├── internal/
│   │   ├── auth/
│   │   ├── rendezvous/
│   │   ├── relay/
│   │   ├── registry/
│   │   ├── billing/
│   │   └── api/
│   └── migrations/
├── desktop/
│   ├── main/
│   ├── preload/
│   ├── renderer/
│   └── tests/
├── web/
├── proto/
├── infra/
│   ├── docker/
│   ├── k8s/
│   └── terraform/
├── docs/
├── scripts/
└── .github/workflows/
```

---

# 4. Phase 0 — Foundation
## Weeks 1–3

### Week 1 — Architecture + repository

| ID | Task | Owner | Pts | Pri |
|---|---|---|---:|---|
| F-01 | Architecture document and component boundaries | Tech Lead | 5 | P0 |
| F-02 | Define threat model assumptions | Security | 5 | P0 |
| F-03 | Create monorepo structure | DevOps | 3 | P0 |
| F-04 | Initialize Rust workspace | Rust | 2 | P0 |
| F-05 | Initialize CMake/C++ project | C++ | 2 | P0 |
| F-06 | Initialize Go module | Go | 1 | P0 |
| F-07 | Initialize Electron + React + TypeScript | Frontend | 3 | P0 |
| F-08 | Establish branch/PR/review rules | Tech Lead | 2 | P1 |

### Week 2 — Contracts + CI

| ID | Task | Owner | Pts | Pri |
|---|---|---|---:|---|
| F-09 | Define protobuf v1 | Core | 5 | P0 |
| F-10 | Define protocol/versioning policy | Core | 3 | P0 |
| F-11 | Configure protobuf code generation | DevOps | 3 | P0 |
| F-12 | Rust Linux/Windows/macOS CI | DevOps | 5 | P0 |
| F-13 | C++ Linux/Windows/macOS CI | DevOps | 5 | P0 |
| F-14 | Go + Electron CI | DevOps | 3 | P0 |
| F-15 | Dependency/security scanning | Security | 3 | P1 |
| F-16 | CONTRIBUTING + FFI rules | Tech Lead | 2 | P1 |

### Week 3 — Cross-language integration

| ID | Task | Owner | Pts | Pri |
|---|---|---|---:|---|
| F-17 | Rust ↔ C++ build integration | DevOps | 8 | P0 |
| F-18 | napi-rs skeleton | Rust | 5 | P0 |
| F-19 | Rust ↔ Node smoke test | Rust + FE | 3 | P0 |
| F-20 | C ABI smoke test | Rust + C++ | 3 | P0 |
| F-21 | Logging/tracing standard | Core | 3 | P1 |
| F-22 | Local developer bootstrap script | DevOps | 3 | P1 |
| F-23 | Reproducible build verification | DevOps | 5 | P0 |

### Gate G0

**Pass criteria:**

- All three operating systems compile required native components.
- Rust can call C++.
- Electron can load Rust native module.
- Protobuf generation is reproducible.
- CI is green.
- Security scanning is active.

---

# 5. Phase 1 — Rust Core
## Weeks 4–10

## Week 4 — Session engine

| ID | Task | Pts | Pri |
|---|---|---:|---|
| R-01 | Tokio runtime | 3 | P0 |
| R-02 | Session state machine | 8 | P0 |
| R-03 | Configuration loader | 2 | P1 |
| R-04 | Error model | 2 | P0 |
| R-05 | Cancellation/shutdown model | 3 | P0 |
| R-06 | Connection lifecycle tests | 5 | P0 |

## Week 5 — Transport

| ID | Task | Pts | Pri |
|---|---|---:|---|
| R-07 | Transport abstraction | 3 | P0 |
| R-08 | TCP transport | 5 | P0 |
| R-09 | QUIC transport | 8 | P0 |
| R-10 | Retry/backoff | 3 | P1 |
| R-11 | Keepalive/heartbeat | 3 | P0 |
| R-12 | Transport integration tests | 5 | P0 |

## Week 6 — Security

| ID | Task | Pts | Pri |
|---|---|---:|---|
| R-13 | TLS 1.3 | 8 | P0 |
| R-14 | Noise/session handshake design | 8 | P0 |
| R-15 | Key exchange/session keys | 5 | P0 |
| R-16 | Secure credential storage interface | 5 | P0 |
| R-17 | Replay protection | 5 | P0 |
| R-18 | Crypto tests + fuzzing | 5 | P0 |

## Week 7 — Protocol/media plumbing

| ID | Task | Pts | Pri |
|---|---|---:|---|
| R-19 | Protobuf codec | 5 | P0 |
| R-20 | Frame reassembly | 8 | P0 |
| R-21 | Jitter/buffer policy | 5 | P0 |
| R-22 | Input event encoding | 3 | P0 |
| R-23 | Backpressure policy | 5 | P0 |

## Week 8 — Vertical slice

| ID | Task | Pts | Pri |
|---|---|---:|---|
| R-24 | CLI client | 5 | P0 |
| R-25 | CLI host | 5 | P0 |
| R-26 | Two-peer connection test | 8 | P0 |
| R-27 | Secure session test | 8 | P0 |
| R-28 | Failure/reconnect tests | 5 | P0 |

### Gate G1

A headless Rust client must:

1. discover a peer using a test/static endpoint,
2. establish a secure session,
3. exchange protocol messages,
4. reconnect after interruption,
5. shut down cleanly.

---

# 6. Phase 2 — C++ Capture + Codec
## Weeks 7–16

> **Important improvement:** Start capture earlier. Do not wait until the Rust layer is finished before proving the media pipeline.

## Week 7–8 — Capture abstraction

| ID | Task | Pts | Pri |
|---|---|---:|---|
| C-01 | Capture interface | 5 | P0 |
| C-02 | Windows capture | 8 | P0 |
| C-03 | Linux capture | 8 | P0 |
| C-04 | macOS capture | 8 | P0 |
| C-05 | Multi-monitor abstraction | 5 | P1 |

## Week 9–10 — Codec integration

> **Do not write a custom H.264 encoder/decoder.** Integrate `libx264` for encode and `FFmpeg`/`libavcodec` (or `OpenH264`) for decode. This is the single highest-leverage correction in this phase: building an in-house codec turns a 24-week plan into a multi-year one for no product benefit — see the decision table in [Section 2](#2-critical-architecture-decisions).

| ID | Task | Pts | Pri |
|---|---|---:|---|
| C-06 | Pixel format abstraction | 3 | P0 |
| C-07 | Frame diff/dirty region detection | 8 | P0 |
| C-08 | YUV conversion | 3 | P0 |
| C-09 | Integrate libx264 encoder + wrapper API | 5 | P0 |
| C-10 | Integrate FFmpeg/libavcodec (or OpenH264) decoder + wrapper API | 5 | P0 |
| C-11 | Circular frame buffer | 5 | P0 |
| C-11a | Codec license/attribution review (see risk register) | 2 | P0 |

## Week 11–12 — Performance

> With encode/decode delegated to `libx264`/`FFmpeg`, they already ship hand-tuned SIMD (SSE2/AVX2/NEON) internally — do not re-implement it. Performance work here is about the pipeline around the codec, not the codec itself.

| ID | Task | Pts | Pri |
|---|---|---:|---|
| C-12 | Zero-copy buffer pipeline (capture → encode → transport) | 8 | P0 |
| C-13 | Encoder tuning presets (latency vs. quality profiles, keyframe interval, bitrate control) | 5 | P0 |
| C-14 | Benchmark suite (encode/decode latency, CPU, bitrate, dropped frames) | 5 | P0 |
| C-15 | Memory allocation profiling | 5 | P0 |

## Week 13–14 — FFI

| ID | Task | Pts | Pri |
|---|---|---:|---|
| C-18 | Stable C ABI | 5 | P0 |
| C-19 | Rust bindgen wrapper | 8 | P0 |
| C-20 | Ownership/lifetime tests | 5 | P0 |
| C-21 | FFI contract tests | 5 | P0 |
| C-22 | FFI fuzz tests | 5 | P0 |

## Week 15–16 — Hardware acceleration

| ID | Task | Pts | Pri |
|---|---|---:|---|
| C-23 | NVENC | 13 | P1 |
| C-24 | VA-API | 8 | P1 |
| C-25 | VideoToolbox | 8 | P1 |
| C-26 | Hardware/software fallback | 5 | P0 |
| C-27 | LAN latency benchmark | 5 | P0 |

### Gate G2

**Target:** 1080p desktop capture → encode → transport → decode → display.

Measure:

- capture latency
- encode latency
- network latency
- decode latency
- render latency
- CPU usage
- memory usage
- bitrate
- dropped frames

Do not accept a single "`<30 ms`" number without defining exactly which latency is being measured.

---

# 7. Phase 3 — Go Control Plane
## Weeks 10–20

## Week 10–12 — Backend foundation

| ID | Task | Pts | Pri |
|---|---|---:|---|
| G-01 | HTTP API | 5 | P0 |
| G-02 | gRPC service | 5 | P0 |
| G-03 | PostgreSQL schema | 5 | P0 |
| G-04 | Migration system | 3 | P0 |
| G-05 | Redis integration | 3 | P0 |
| G-06 | Health/readiness endpoints | 3 | P0 |
| G-07 | Structured logging | 3 | P0 |

## Week 13–14 — Rendezvous

| ID | Task | Pts | Pri |
|---|---|---:|---|
| G-08 | Device registration | 5 | P0 |
| G-09 | Endpoint registry | 5 | P0 |
| G-10 | Peer lookup | 5 | P0 |
| G-11 | Heartbeats/TTL | 3 | P0 |
| G-12 | Session authorization | 8 | P0 |

## Week 15–16 — Authentication

| ID | Task | Pts | Pri |
|---|---|---:|---|
| G-13 | Account model | 5 | P0 |
| G-14 | JWT/session authentication | 8 | P0 |
| G-15 | Refresh-token rotation | 5 | P0 |
| G-16 | 2FA/TOTP | 5 | P0 |
| G-17 | Device authorization | 5 | P0 |
| G-18 | Rate limiting | 5 | P0 |
| G-19 | Audit events | 5 | P0 |

## Week 17–18 — Relay

| ID | Task | Pts | Pri |
|---|---|---:|---|
| G-20 | Relay service | 13 | P0 |
| G-21 | Relay authentication | 5 | P0 |
| G-22 | Bandwidth accounting | 5 | P1 |
| G-23 | Connection quotas | 5 | P1 |
| G-24 | Relay observability | 5 | P0 |

## Week 19–20 — Scale

| ID | Task | Pts | Pri |
|---|---|---:|---|
| G-25 | Docker images | 3 | P0 |
| G-26 | Prometheus metrics | 5 | P0 |
| G-27 | Grafana dashboards | 5 | P0 |
| G-28 | Load test 10k connections | 8 | P0 |
| G-29 | Load test 100k connections | 13 | P1 |
| G-30 | Horizontal scaling test | 8 | P0 |
| G-31 | Multi-region design | 5 | P1 |

### Gate G3

A desktop peer must be able to:

```text
Login
  ↓
Register device
  ↓
Discover peer
  ↓
Attempt direct connection
  ↓
If direct fails → relay
  ↓
Establish secure session
```

---

# 8. Phase 4 — Electron Desktop MVP
## Weeks 14–24

## Week 14–16 — Native integration

| ID | Task | Pts | Pri |
|---|---|---:|---|
| E-01 | napi-rs Rust binding | 8 | P0 |
| E-02 | IPC architecture | 5 | P0 |
| E-03 | TypeScript type generation | 5 | P0 |
| E-04 | Native crash handling | 5 | P0 |
| E-05 | Native module loading diagnostics | 3 | P0 |

## Week 17–18 — Session UX

| ID | Task | Pts | Pri |
|---|---|---:|---|
| E-06 | Login | 5 | P0 |
| E-07 | Device ID/connection screen | 5 | P0 |
| E-08 | Session status | 3 | P0 |
| E-09 | Connection errors | 5 | P0 |
| E-10 | Settings | 3 | P1 |
| E-11 | Address book | 5 | P1 |

## Week 19–20 — Video/input

| ID | Task | Pts | Pri |
|---|---|---:|---|
| E-12 | GPU-friendly frame path | 13 | P0 |
| E-13 | Shared-memory/native buffer strategy | 8 | P0 |
| E-14 | Keyboard input | 5 | P0 |
| E-15 | Mouse input | 5 | P0 |
| E-16 | Scroll input | 3 | P0 |
| E-17 | Multi-monitor selection | 5 | P1 |

## Week 21–22 — Packaging

| ID | Task | Pts | Pri |
|---|---|---:|---|
| E-18 | Tray integration | 3 | P1 |
| E-19 | Notifications | 3 | P1 |
| E-20 | Auto-update | 5 | P1 |
| E-21 | Windows signing | 5 | P0 |
| E-22 | macOS signing/notarization | 8 | P0 |
| E-23 | Linux packaging | 5 | P0 |

## Week 23–24 — QA

| ID | Task | Pts | Pri |
|---|---|---:|---|
| E-24 | Playwright E2E | 8 | P0 |
| E-25 | Cross-platform QA | 8 | P0 |
| E-26 | Native crash testing | 5 | P0 |
| E-27 | Network interruption tests | 5 | P0 |
| E-28 | Performance regression tests | 5 | P0 |
| E-29 | Bug bash | 8 | P0 |

### Gate G4 — Desktop MVP

MVP must support:

- install
- login
- device registration
- peer discovery
- direct connection
- relay fallback
- secure session
- screen viewing
- keyboard/mouse control
- disconnect/reconnect
- logs/diagnostics
- signed installer

---

# 9. Phase 5 — Production Features
## Weeks 24–34

| Week | Feature | Pri |
|---:|---|---|
| 24–25 | File transfer | P0 |
| 26 | Clipboard sync | P1 |
| 27 | Multi-monitor improvements | P1 |
| 28–29 | Audio streaming | P1 |
| 30 | Session recording | P1 |
| 31 | In-session chat | P2 |
| 32 | Wake-on-LAN | P2 |
| 33 | Hardware encoder optimization | P1 |
| 34 | Performance/tuning sprint | P0 |

## Feature gate

Each feature must include:

- protocol changes
- authorization
- auditability where applicable
- bandwidth/backpressure behavior
- unit tests
- integration tests
- failure handling
- UI tests
- performance impact measurement

---

# 10. Phase 6 — Web Client
## Weeks 32–38

| ID | Task | Owner | Pts | Pri |
|---|---|---|---:|---|
| W-01 | Define browser security model | Security | 5 | P0 |
| W-02 | Rust/WASM feasibility prototype | Rust | 8 | P0 |
| W-03 | Browser transport prototype | Rust | 8 | P0 |
| W-04 | WebRTC integration | Core | 13 | P0 |
| W-05 | Thin web UI | FE | 8 | P0 |
| W-06 | Browser input handling | FE | 5 | P0 |
| W-07 | Browser compatibility testing | QA | 5 | P0 |
| W-08 | CDN deployment | DevOps | 3 | P1 |

> **Architecture gate:** Do not commit the entire web client to WASM until a prototype proves that browser APIs, codec requirements, shared-memory assumptions, and security constraints are viable.

---

# 11. Phase 8 — Security, Reliability & Launch
## Weeks 38–46

> Mobile is out of scope (see Section 30's changelog), so this phase now follows directly after the web client (Section 10) instead of waiting on a separate mobile track — pulling the whole phase in by 6 weeks.

| Week | Task | Owner | Pri |
|---:|---|---|---|
| 38–40 | External security audit | Vendor | P0 |
| 41 | Penetration test | Security | P0 |
| 41 | Remediation | All | P0 |
| 42 | Full performance tuning | All | P0 |
| 43 | Disaster/recovery testing | DevOps | P0 |
| 43 | Developer documentation | Tech Lead | P1 |
| 44 | User documentation | PM + FE | P1 |
| 44 | Marketing/landing page | PM + FE | P2 |
| 45 | Private beta | All | P0 |
| 45 | Beta telemetry review | SRE | P0 |
| 46 | Public v1.0 | All | P0 |

---

# 12. Security Workstream
## Runs from Week 1, not Week 44

| Area | Requirement |
|---|---|
| Identity | Device and user identity must be distinct |
| Authentication | Strong authentication + refresh-token rotation |
| Authorization | Explicit permission for remote-control actions |
| Encryption | TLS/secure session encryption |
| Key storage | OS secure storage where available |
| Replay | Nonces/counters/session binding |
| Abuse | Rate limits, quotas, lockouts |
| Audit | Connection and authorization events |
| Updates | Signed application updates |
| Native code | Fuzz FFI boundaries |
| Secrets | Never commit credentials/private keys |
| Supply chain | Dependency scanning and lockfiles |
| Privacy | Minimize telemetry and define retention |
| Relay | Authenticate before allocating relay resources |

## Security gates

- [ ] Threat model reviewed
- [ ] Authentication tested
- [ ] Authorization tested
- [ ] Crypto review completed
- [ ] Fuzzing active
- [ ] Dependency scanning active
- [ ] Native crash reporting active
- [ ] Penetration test passed
- [ ] Critical findings resolved

---

# 13. Observability

Every production component should expose:

## Metrics

```text
connections_total
connections_active
connections_failed_total
connection_latency_ms
session_duration_seconds
bytes_sent
bytes_received
frames_captured
frames_encoded
frames_decoded
frames_dropped
encode_latency_ms
decode_latency_ms
relay_sessions_active
relay_bandwidth_bytes
auth_failures_total
```

## Logs

Use structured logs with:

```text
timestamp
service
component
session_id
device_id
event
severity
error_code
latency_ms
```

Never log:

- passwords
- private keys
- access tokens
- raw session secrets

---

# 14. Performance Budgets

Define measurable budgets instead of vague "fast" requirements.

| Metric | Initial target |
|---|---:|
| LAN connection establishment | < 500 ms |
| Interactive input latency | < 100 ms target |
| Capture → display latency | < 100 ms target |
| Frame delivery | ≥ 30 FPS under normal load |
| Target FPS | 60 FPS where hardware permits |
| CPU idle overhead | measured and bounded |
| Memory growth | no unbounded growth |
| Reconnect | < 5 s target |
| Relay availability | ≥ 99.9% target |
| API p95 | define per endpoint |
| Crash-free sessions | ≥ 99.9% target |

> These are engineering targets, not guarantees. Benchmark on representative hardware and network conditions.

---

# 15. Testing Strategy

## Unit

Every component:

```text
Rust
 ├─ session
 ├─ transport
 ├─ crypto
 ├─ protocol
 └─ input

C++
 ├─ capture
 ├─ codec
 ├─ buffer
 └─ pixel conversion

Go
 ├─ auth
 ├─ registry
 ├─ rendezvous
 ├─ relay
 └─ API
```

## Integration

Required boundaries:

```text
Rust ↔ C++
Rust ↔ Node
Rust ↔ Go
Go ↔ PostgreSQL
Go ↔ Redis
Electron main ↔ renderer
```

## E2E

At minimum:

1. Two desktop clients on LAN
2. Two clients across the Internet
3. NAT-to-NAT
4. Direct failure → relay fallback
5. Network interruption → reconnect
6. High packet loss
7. High latency
8. Multiple monitors
9. Large file transfer
10. Installer/update flow

## Fuzzing

Fuzz:

- protocol decoder
- frame parser
- protobuf input
- C ABI functions
- codec input
- network packet boundaries
- malformed authentication messages

---

# 16. Load Testing

## Test levels

### Level 1 — Functional

```text
2 peers
10 peers
100 peers
```

### Level 2 — Control plane

```text
1k concurrent devices
10k concurrent devices
100k concurrent devices
```

### Level 3 — Relay

Measure:

```text
concurrent sessions
Gbps throughput
CPU/session
memory/session
connection establishment rate
relay failure rate
```

### Level 4 — Soak

Run:

```text
24 hours
72 hours
7 days
```

Check:

- memory leaks
- goroutine/thread growth
- connection leaks
- Redis growth
- PostgreSQL growth
- file descriptor exhaustion
- reconnect storms

---

# 17. Failure Engineering

Test intentionally:

- backend unavailable
- Redis unavailable
- PostgreSQL unavailable
- relay unavailable
- DNS failure
- expired authentication
- invalid peer ID
- corrupted frame
- packet loss
- packet reordering
- network switch
- laptop sleep/wake
- application crash
- native library crash
- update interruption

Every failure should have:

```text
Detection → Logging → Recovery → User feedback → Metrics
```

---

# 18. CI/CD Pipeline

```text
Pull Request
    │
    ├── Format
    ├── Lint
    ├── Unit Tests
    ├── Integration Tests
    ├── Security Scan
    ├── Fuzz Smoke Test
    ├── Build Native Libraries
    └── Build Electron
             │
             ▼
        Main Branch
             │
             ├── Full E2E
             ├── Cross-platform builds
             ├── Performance regression
             └── Artifact signing
                     │
                     ▼
                 Release
```

## Release channels

```text
nightly
   ↓
dev
   ↓
alpha
   ↓
beta
   ↓
stable
```

---

# 19. Infrastructure

## Development

```text
Docker Compose
├── backend
├── postgres
├── redis
├── prometheus
└── grafana
```

## Production

```text
Load Balancer
      │
      ├── API instances
      ├── Rendezvous instances
      └── Relay instances
             │
             ├── Redis
             └── PostgreSQL
```

## Kubernetes

Do not introduce Kubernetes merely because it is available.

First prove:

- containerized deployment
- health checks
- horizontal scaling
- observability
- backup/restore
- operational procedures

Then deploy to Kubernetes when scale/operations justify it.

---

# 20. FFI Governance

## Rust ↔ C++

- `extern "C"` only
- `#[repr(C)]` for shared structs
- opaque handles
- explicit ownership
- explicit `_free`
- no Rust references across ABI
- no C++ exceptions across ABI
- no Rust panics across ABI
- ABI version check
- documented buffer lifetime
- fuzz entrypoints
- contract tests

## Rust ↔ Electron

- napi-rs
- async operations must not block Node
- background callbacks through thread-safe mechanisms
- minimize JS/native copies
- explicit native resource lifecycle
- generated TypeScript types
- native crash diagnostics

## Rust ↔ Go

This is a **network boundary**, not FFI.

Use:

- protobuf
- gRPC
- TLS
- explicit API versioning
- authentication
- deadlines/timeouts
- retry policy
- backward compatibility

---

# 21. FFI Contract Example

The C ABI should remain stable and explicit:

```c
typedef enum {
    ND_CODEC_OK = 0,
    ND_CODEC_ERR_INVALID_ARG = 1,
    ND_CODEC_ERR_NO_MEMORY = 2,
    ND_CODEC_ERR_ENCODE = 3,
    ND_CODEC_ERR_DECODE = 4,
    ND_CODEC_ERR_UNSUPPORTED = 5,
    ND_CODEC_ERR_INTERNAL = 6
} nd_codec_status;
```

Ownership must be documented for every pointer:

```text
borrowed → callee does not free
owned    → caller owns returned memory
```

Every `create` must have a corresponding `free`.

---

# 22. Protocol Versioning

Never silently break a deployed client.

```text
proto/
├── nexdesk/v1/
│   ├── session.proto
│   ├── rendezvous.proto
│   └── relay.proto
└── nexdesk/v2/
```

Compatibility rules:

- Never reuse field numbers.
- Prefer additive changes.
- Maintain v1 while v2 clients migrate.
- Test old-client/new-server compatibility.
- Test new-client/old-server compatibility where supported.

---

# 23. Weekly Engineering Ritual

Every week:

### Monday

- Sprint planning
- Dependency review
- Risk review

### Daily

- Build/CI status
- Blockers
- Production issues

### Wednesday

- Architecture review for critical changes

### Friday

- Demo working software
- Update metrics
- Run regression suite
- Update roadmap

### End of sprint

Record:

```text
Completed
Not completed
Blocked
Technical debt
Security findings
Performance results
Next sprint dependencies
```

---

# 24. Risk Register

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| NAT traversal complexity | High | High | Prototype early; relay fallback |
| Native FFI crashes | High | Medium | Stable ABI + fuzzing + contract tests |
| Electron frame-copy overhead | High | Medium | Native/GPU buffer strategy |
| Codec portability | High | Medium | Software fallback |
| Custom codec scope creep | Critical | Medium | Wrap `libx264`/`FFmpeg`/hardware encoders instead of building an in-house H.264 codec (see [Section 6](#6-phase-2--c-capture--codec)) |
| H.264 patent licensing (MPEG-LA) for commercial distribution | Medium | Medium | Budget for licensing before public launch, or plan an AV1/VP9 royalty-free path as a longer-term fallback |
| Unstated team size invalidates the schedule | High | High | Confirm actual headcount against [Section 0's assumptions](#0-executive-plan) before committing to milestone dates |
| Browser limitations | High | High | Early WebRTC feasibility prototype |
| Relay bandwidth cost | High | High | Measure cost/session early |
| Security vulnerability | Critical | Medium | Threat model from Week 1 |
| Cross-platform CI instability | Medium | Medium | Build matrix from Week 2 |
| Scope growth | High | High | MVP feature freeze |
| Memory leaks | High | Medium | Soak tests + profiling |
| Protocol incompatibility | High | Medium | Versioned protobuf |
| Dependency vulnerabilities | High | Medium | Automated scanning |

---

# 25. MVP Scope Freeze

## Must have

- Account/authentication
- Device registration
- Peer discovery
- Secure connection
- Direct connection
- Relay fallback
- Screen capture
- Screen encoding/decoding
- Screen rendering
- Keyboard input
- Mouse input
- Reconnect
- Diagnostics
- Signed desktop installers
- Auto-update foundation
- Basic audit/security controls

## Not required for MVP

- Audio
- Chat
- Session recording
- Wake-on-LAN
- Advanced file transfer
- Web client
- Advanced hardware tuning

This prevents the MVP from becoming a one-year prototype.

---

# 26. Critical Path

The true critical path is:

```text
Architecture
    ↓
Protocol contracts
    ↓
Rust session/transport
    ↓
Secure peer connection
    ↓
Capture
    ↓
C++ codec
    ↓
Rust ↔ C++ FFI
    ↓
Frame transport
    ↓
Electron native integration
    ↓
Input control
    ↓
Rendezvous
    ↓
NAT traversal
    ↓
Relay fallback
    ↓
Desktop MVP
```

Work outside this path should not block the first usable product unless it is security-critical.

---

# 27. Milestone Acceptance Matrix

| Milestone | Acceptance test |
|---|---|
| M0 | Clean checkout builds on Win/Mac/Linux |
| M1 | Two headless peers establish secure session |
| M2 | Real desktop screen displayed over LAN |
| M3 | Internet session works with relay fallback |
| M4 | User installs and controls another desktop |
| M5 | Production desktop feature set passes regression |
| M6 | Browser connects to desktop |
| M7 | Security/performance/reliability gates passed |

---

# 28. Final Release Checklist

## Product

- [ ] MVP requirements complete
- [ ] UX reviewed
- [ ] Error states implemented
- [ ] Accessibility reviewed

## Security

- [ ] Threat model updated
- [ ] Pen test complete
- [ ] Critical/high findings resolved
- [ ] Secrets audit complete
- [ ] Signing keys protected

## Reliability

- [ ] 24h soak test
- [ ] 72h soak test
- [ ] Backup/restore tested
- [ ] Relay failure tested
- [ ] Reconnect tested
- [ ] Crash recovery tested

## Performance

- [ ] CPU benchmark
- [ ] Memory benchmark
- [ ] Encode/decode benchmark
- [ ] Input latency benchmark
- [ ] Network-loss benchmark
- [ ] Relay throughput benchmark

## Release

- [ ] Windows installer signed
- [ ] macOS package signed/notarized
- [ ] Linux packages built
- [ ] Update channel tested
- [ ] Rollback procedure tested
- [ ] Documentation published
- [ ] Monitoring enabled
- [ ] Incident response procedure ready

---

# 29. Recommended Execution Order

If working with a small team, do **not** execute all phases strictly one after another.

Use parallel tracks:

```text
TRACK A — Rust/Core
Weeks 1–10
Architecture → Session → Transport → Security → Protocol

TRACK B — Codec
Weeks 3–14
Capture → Encode → Decode → FFI → Performance

TRACK C — Backend
Weeks 5–20
API → Auth → Registry → Rendezvous → Relay → Scale

TRACK D — Desktop
Weeks 10–24
Electron → napi-rs → Video → Input → Packaging → QA

TRACK E — Security/SRE
Weeks 1–46
Threat model → scanning → observability → fuzzing → audits

TRACK F — Web
After desktop architecture is proven
Web → WebRTC → browser compatibility → CDN deployment
```

---

# 30. The Most Important Planner Changes

Compared with the original plan, this version intentionally:

1. **Moves security to Week 1** instead of treating it mainly as a launch activity.
2. **Builds a vertical slice earlier**, so architecture problems are discovered before months of work.
3. **Starts codec/capture earlier**, because remote desktop performance depends heavily on the media path.
4. **Adds an explicit architecture gate** before Web.
5. **Adds observability as a first-class workstream**, not a late dashboard task.
6. **Adds failure engineering and soak testing**.
7. **Defines measurable performance budgets** instead of relying on vague latency claims.
8. **Adds protocol compatibility/versioning rules**.
9. **Separates MVP requirements from post-MVP features**.
10. **Adds a real critical path and milestone acceptance matrix**.
11. **Adds supply-chain/security controls** for a security-sensitive remote-access product.
12. **Makes every FFI boundary testable and ownership-explicit**.
13. **Introduces release channels and rollback planning**.
14. **Avoids forcing Kubernetes or WASM before the architecture proves they are appropriate**.
15. **States explicit team-size and velocity assumptions** ([Section 0](#0-executive-plan)) so the week numbers can be sanity-checked against actual headcount instead of being read as unconditional dates.
16. **Replaces the in-house H.264 encoder/decoder/SIMD build with library integration** (`libx264`/`FFmpeg`/hardware encoders — [Section 6](#6-phase-2--c-capture--codec)), since writing a competitive codec from scratch was budgeted at roughly a hundredth of the effort it actually requires.
17. **Adds a table of contents** given the planner's length.
18. **Descopes mobile entirely.** The former Phase 7 (iOS/Android), the mobile gate, milestone M7, the "Mobile platform restrictions" risk, and Track F's mobile leg are all removed — this product ships as desktop + web only. Removing mobile's tail also let Phase 8 (Security, Reliability & Launch) start 6 weeks earlier, since it no longer needs to wait on a track that ran in parallel to it; total timeline drops from 52 to 46 weeks. If mobile clients become a real requirement later, treat it as a new phase added after the v1.0 launch gate, not a resurrection of the old Phase 7 — re-derive its scope and schedule from where the product actually stands then.

---

# 31. Success Definition

The project is successful when a new developer can clone the repository, run one bootstrap command, build the native components, start the backend, launch two desktop clients, authenticate, discover a peer, establish a secure direct connection, fall back to a relay when necessary, view the remote desktop with acceptable latency, control keyboard/mouse input, disconnect/reconnect safely, and observe the entire session through logs and metrics.

**Primary engineering rule:**

> **Prove the complete path early. Optimize and expand only after the complete path works.**
