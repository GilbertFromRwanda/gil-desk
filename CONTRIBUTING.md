# Contributing

## Branching / PRs (planner task F-08)

- Branch from `main`; name branches `<area>/<short-description>` (e.g. `core/session-state-machine`).
- One logical change per PR. Keep the PR description's "why" over "what" — the diff shows what changed.
- A PR is not mergeable until its Definition of Done items (planner Section 0) are satisfied for the change it makes.

## FFI rules (planner task F-16 / Section 21)

Any change that touches the Rust↔C++ boundary (`core/ffi`, `codec/include`) or the Rust↔Electron boundary (`desktop/preload`, napi bindings) must:

- Keep the C ABI `extern "C"` only, with `#[repr(C)]` for shared structs and opaque handles.
- Never let a Rust panic or a C++ exception cross the boundary.
- Document ownership (`borrowed` vs `owned`) for every pointer, and pair every `create` with a `free`.
- Bump `nd_ffi_abi_version()` / `nd_codec_abi_version()` on any breaking change to the boundary.

The Rust↔Go boundary is a network boundary (protobuf/gRPC over TLS), not FFI — see planner Section 21.

## Codec changes

Do not add a hand-written video encoder/decoder to `codec/`. Encode/decode wraps `libx264` and `FFmpeg`/`libavcodec` (or hardware encoders) — see the planner's Section 2 decision table and Section 6 for why.
