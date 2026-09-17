// Electron main process entrypoint (planner tasks E-01..E-05).
import { app, BrowserWindow, ipcMain, Menu } from "electron";
import path from "node:path";
import * as authClient from "./authClient";
import * as rendezvousClient from "./rendezvousClient";
import { loadOrCreateDeviceIdentity } from "./deviceIdentity";
import { bootstrapAccessToken } from "./accountIdentity";
import native from "./nativeBridge";
import type { DecodedInputEvent, RelaySession } from "./nativeBridge";
import { randomUUID } from "node:crypto";

// No menu bar — this app has no File/Edit/View functionality a native
// menu would meaningfully expose, and it was only ever there because
// BrowserWindow creates one by default, not by design.
Menu.setApplicationMenu(null);

const backendConfig = { baseUrl: process.env.NEXDESK_BACKEND_URL ?? "http://localhost:8080" };
const grpcAddr = process.env.NEXDESK_GRPC_ADDR ?? "localhost:9090";
// The relay (backend/internal/relay) runs on the same grpc.Server as
// rendezvous — same host:port as grpcAddr above, just needs a URI scheme
// for tonic::transport::Channel (rendezvousClient's @grpc/grpc-js side
// doesn't need one).
const relayAddr = process.env.NEXDESK_RELAY_ADDR ?? `http://${grpcAddr}`;

function createWindow(): void {
  const win = new BrowserWindow({
    width: 1024,
    height: 720,
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  win.loadURL(
    process.env.NEXDESK_RENDERER_URL ?? `file://${path.join(__dirname, "../renderer/index.html")}`
  );
}

// On-demand account login/register (planner E-06) — separate from the
// automatic per-device account accountIdentity.ts bootstraps for
// RegisterDevice/RequestSession, which needs no user interaction at all.
// This is for a person who explicitly wants to log into (or create) a
// real account of their own, e.g. for a future paid tier — reachable
// only via ConnectScreen's "Log in" button, never required to use the
// app. Handlers just forward to authClient and return its result
// verbatim, same as before this was on-demand rather than the only path.
ipcMain.handle("auth:register", (_event, email: string, password: string) =>
  authClient.register(backendConfig, email, password)
);
ipcMain.handle("auth:login", (_event, email: string, password: string) =>
  authClient.login(backendConfig, email, password)
);
ipcMain.handle("auth:loginTwoFactor", (_event, pendingToken: string, code: string) =>
  authClient.loginTwoFactor(backendConfig, pendingToken, code)
);

// device:getIdentity doesn't need `app` to be ready (it's called once the
// window's renderer loads, well after whenReady), but loadOrCreateDeviceIdentity
// itself calls app.getPath — safe here since IPC handlers only ever run
// after a renderer exists, i.e. after createWindow, i.e. after whenReady.
ipcMain.handle("device:getIdentity", () => loadOrCreateDeviceIdentity());

// The renderer never sees an access token at all now — there's no login
// screen for it to come from a form submission anymore, so main fetches
// one (bootstrapAccessToken caches it in memory) whenever a call
// actually needs it, rather than the renderer holding and passing one.
ipcMain.handle("device:register", async () => {
  const identity = loadOrCreateDeviceIdentity();
  const accessToken = await bootstrapAccessToken(backendConfig);
  return rendezvousClient.registerDevice(grpcAddr, accessToken, identity.deviceId, identity.publicKeyPem);
});

ipcMain.handle("device:requestSession", async (_event, targetDeviceId: string) => {
  const identity = loadOrCreateDeviceIdentity();
  const accessToken = await bootstrapAccessToken(backendConfig);
  return rendezvousClient.requestSession(grpcAddr, accessToken, identity.deviceId, targetDeviceId);
});

// Encodes a captured DOM input event through the real native addon, then
// immediately decodes what it just produced through the same addon
// (planner E-14/E-15/E-16) — the renderer never fabricates the "it round
// tripped" claim itself, main does the encode *and* the decode and hands
// back only what the native module actually returned.
interface CapturedEvent {
  kind: "keyDown" | "keyUp" | "mouseMove" | "mouseButton" | "scroll";
  code?: number;
  x?: number;
  y?: number;
  button?: number;
  down?: boolean;
  dx?: number;
  dy?: number;
}

ipcMain.handle("input:roundTrip", (_event, captured: CapturedEvent): { encodedHex: string; decoded: DecodedInputEvent } => {
  let encoded: Buffer;
  switch (captured.kind) {
    case "keyDown":
      encoded = native.encodeKeyDown(captured.code ?? 0);
      break;
    case "keyUp":
      encoded = native.encodeKeyUp(captured.code ?? 0);
      break;
    case "mouseMove":
      encoded = native.encodeMouseMove(captured.x ?? 0, captured.y ?? 0);
      break;
    case "mouseButton":
      encoded = native.encodeMouseButton(captured.button ?? 0, captured.down ?? false);
      break;
    case "scroll":
      encoded = native.encodeScroll(captured.dx ?? 0, captured.dy ?? 0);
      break;
  }
  return { encodedHex: encoded.toString("hex"), decoded: native.decodeInputEvent(encoded) };
});

// A RelaySession is a live native object (holds a real Rust connection)
// — it can't cross IPC to the renderer directly, so main keeps it here
// keyed by an opaque id and the renderer only ever holds that id, the
// same pattern deviceIdentity.ts's cache uses for a different reason.
const relaySessions = new Map<string, RelaySession>();

ipcMain.handle("relay:connect", async (_event, sessionToken: string) => {
  const identity = loadOrCreateDeviceIdentity();
  const session = await native.connectRelaySession(relayAddr, sessionToken, identity.deviceId);
  const sessionId = randomUUID();
  relaySessions.set(sessionId, session);
  return { sessionId, peerDeviceId: session.peerDeviceId };
});

ipcMain.handle("relay:send", async (_event, sessionId: string, text: string) => {
  const session = relaySessions.get(sessionId);
  if (!session) throw new Error("unknown relay session");
  await session.send(Buffer.from(text, "utf8"));
});

ipcMain.handle("relay:recv", async (_event, sessionId: string) => {
  const session = relaySessions.get(sessionId);
  if (!session) throw new Error("unknown relay session");
  const bytes = await session.recv();
  return bytes.toString("utf8");
});

app.whenReady().then(createWindow);

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow();
  }
});
