// Loads the compiled napi-rs addon (desktop/native, planner F-18/F-19)
// into the actual Electron main process for the first time — until now
// it had only ever been exercised by native/smoke-test.js under plain
// Node. N-API (the `napi4` feature in native/Cargo.toml) is specifically
// designed to be ABI-stable across Node and Electron builds, so this is
// expected to load the same compiled index.node without a separate
// Electron-specific rebuild — verified by actually launching the app,
// not just assumed from napi-rs's docs.
import path from "node:path";

interface DecodedInputEvent {
  kind: "keyDown" | "keyUp" | "mouseMove" | "mouseButton" | "scroll";
  code?: number;
  x?: number;
  y?: number;
  button?: number;
  down?: boolean;
  dx?: number;
  dy?: number;
}

// A live session with a real peer, connected through the relay
// (nexdesk-core's rendezvous::relay, Gate G3). One JS object per
// connected session, backed by a real Rust RelayConnection held alive by
// the native addon for as long as this object is referenced.
export interface RelaySession {
  readonly peerDeviceId: string;
  send(data: Buffer): Promise<void>;
  recv(): Promise<Buffer>;
}

interface NativeAddon {
  nativeVersion(): string;
  initialSessionState(): string;
  encodeKeyDown(code: number): Buffer;
  encodeKeyUp(code: number): Buffer;
  encodeMouseMove(x: number, y: number): Buffer;
  encodeMouseButton(button: number, down: boolean): Buffer;
  encodeScroll(dx: number, dy: number): Buffer;
  decodeInputEvent(bytes: Buffer): DecodedInputEvent;
  connectRelaySession(relayAddr: string, sessionToken: string, deviceId: string): Promise<RelaySession>;
}

const native: NativeAddon = require(path.join(__dirname, "../../native/index.node"));

export type { DecodedInputEvent };
export default native;
