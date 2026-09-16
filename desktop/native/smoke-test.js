// Rust <-> Node smoke test (planner task F-19). Requires index.node to have
// been built first — see build.ps1/build.sh in this directory.
const assert = require("node:assert");
const native = require("./index.node");

assert.strictEqual(typeof native.nativeVersion(), "string");
assert.strictEqual(native.nativeVersion(), "0.1.0");
assert.strictEqual(native.initialSessionState(), "idle");

// Input event encode/decode (planner E-14/E-15/E-16): round-trip every
// variant through the real compiled addon, not just trust the Rust-side
// unit tests that already cover InputEvent::encode/decode in isolation.
const keyDown = native.decodeInputEvent(native.encodeKeyDown(65));
assert.strictEqual(keyDown.kind, "keyDown");
assert.strictEqual(keyDown.code, 65);

const keyUp = native.decodeInputEvent(native.encodeKeyUp(65));
assert.strictEqual(keyUp.kind, "keyUp");
assert.strictEqual(keyUp.code, 65);

const move = native.decodeInputEvent(native.encodeMouseMove(-12, 900));
assert.strictEqual(move.kind, "mouseMove");
assert.strictEqual(move.x, -12);
assert.strictEqual(move.y, 900);

const button = native.decodeInputEvent(native.encodeMouseButton(1, true));
assert.strictEqual(button.kind, "mouseButton");
assert.strictEqual(button.button, 1);
assert.strictEqual(button.down, true);

const scroll = native.decodeInputEvent(native.encodeScroll(0, -3));
assert.strictEqual(scroll.kind, "scroll");
assert.strictEqual(scroll.dx, 0);
assert.strictEqual(scroll.dy, -3);

assert.throws(() => native.decodeInputEvent(Buffer.from([99, 0, 0, 0, 0])));

console.log("smoke-test: ok —", JSON.stringify({
  nativeVersion: native.nativeVersion(),
  initialSessionState: native.initialSessionState(),
  inputRoundTrips: { keyDown, keyUp, move, button, scroll },
}));
