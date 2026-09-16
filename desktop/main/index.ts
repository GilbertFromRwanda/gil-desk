// Electron main process entrypoint (planner tasks E-01..E-05).
import { app, BrowserWindow, ipcMain } from "electron";
import path from "node:path";
import * as authClient from "./authClient";
import * as rendezvousClient from "./rendezvousClient";
import { loadOrCreateDeviceIdentity } from "./deviceIdentity";
import native from "./nativeBridge";
import type { DecodedInputEvent } from "./nativeBridge";

const backendConfig = { baseUrl: process.env.NEXDESK_BACKEND_URL ?? "http://localhost:8080" };
const grpcAddr = process.env.NEXDESK_GRPC_ADDR ?? "localhost:9090";

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

// Every handler below just forwards to authClient and returns its result
// verbatim — the renderer (via preload's contextBridge, see
// preload/index.ts) gets the same {ok, status, body} shape the backend
// actually returned, so error messages the Go server wrote (e.g. "invalid
// email or password") reach the UI unchanged instead of being redecided
// here.
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

ipcMain.handle("device:register", (_event, accessToken: string) => {
  const identity = loadOrCreateDeviceIdentity();
  return rendezvousClient.registerDevice(grpcAddr, accessToken, identity.deviceId, identity.publicKeyPem);
});

ipcMain.handle("device:requestSession", (_event, accessToken: string, targetDeviceId: string) => {
  const identity = loadOrCreateDeviceIdentity();
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
