// Rust <-> Node smoke test (planner task F-19). Requires index.node to have
// been built first — see build.ps1/build.sh in this directory.
const assert = require("node:assert");
const native = require("./index.node");

assert.strictEqual(typeof native.nativeVersion(), "string");
assert.strictEqual(native.nativeVersion(), "0.1.0");
assert.strictEqual(native.initialSessionState(), "idle");

console.log("smoke-test: ok —", JSON.stringify({
  nativeVersion: native.nativeVersion(),
  initialSessionState: native.initialSessionState(),
}));
