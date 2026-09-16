// Preload bridge — explicit, minimal API surface exposed to the renderer
// (planner Section 21, "Rust ↔ Electron": minimize JS/native copies).
// The renderer never gets `ipcRenderer` or `fetch`-to-backend directly —
// only these three named, single-purpose calls, matching the same
// least-privilege posture as contextIsolation/nodeIntegration above it.
import { contextBridge, ipcRenderer } from "electron";

// Mirrors main/authClient.ts's AuthResult. Duplicated rather than
// imported: preload and main compile as separate TS projects (different
// rootDir/outDir), and this shape is small enough that keeping the two in
// sync by eye is simpler than wiring a shared project reference for it.
interface AuthResult {
  ok: boolean;
  status: number;
  body: unknown;
}

interface DeviceIdentity {
  deviceId: string;
  publicKeyPem: string;
}

interface RegisterDeviceResult {
  ok: boolean;
  message: string;
}

interface RequestSessionResult {
  ok: boolean;
  authorized: boolean;
  sessionToken: string;
  expiresInSeconds: number;
  message: string;
}

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

interface DecodedInputEvent {
  kind: string;
  code?: number;
  x?: number;
  y?: number;
  button?: number;
  down?: boolean;
  dx?: number;
  dy?: number;
}

interface InputRoundTripResult {
  encodedHex: string;
  decoded: DecodedInputEvent;
}

interface RelayConnectResult {
  sessionId: string;
  peerDeviceId: string;
}

contextBridge.exposeInMainWorld("nexdesk", {
  version: "0.1.0",
  auth: {
    register: (email: string, password: string): Promise<AuthResult> =>
      ipcRenderer.invoke("auth:register", email, password),
    login: (email: string, password: string): Promise<AuthResult> =>
      ipcRenderer.invoke("auth:login", email, password),
    loginTwoFactor: (pendingToken: string, code: string): Promise<AuthResult> =>
      ipcRenderer.invoke("auth:loginTwoFactor", pendingToken, code),
  },
  device: {
    getIdentity: (): Promise<DeviceIdentity> => ipcRenderer.invoke("device:getIdentity"),
    register: (accessToken: string): Promise<RegisterDeviceResult> =>
      ipcRenderer.invoke("device:register", accessToken),
    requestSession: (accessToken: string, targetDeviceId: string): Promise<RequestSessionResult> =>
      ipcRenderer.invoke("device:requestSession", accessToken, targetDeviceId),
  },
  input: {
    roundTrip: (event: CapturedEvent): Promise<InputRoundTripResult> =>
      ipcRenderer.invoke("input:roundTrip", event),
  },
  relay: {
    connect: (sessionToken: string): Promise<RelayConnectResult> =>
      ipcRenderer.invoke("relay:connect", sessionToken),
    send: (sessionId: string, text: string): Promise<void> =>
      ipcRenderer.invoke("relay:send", sessionId, text),
    recv: (sessionId: string): Promise<string> => ipcRenderer.invoke("relay:recv", sessionId),
  },
});
