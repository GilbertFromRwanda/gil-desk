// Per-install device identity (planner task E-07's prerequisite: a
// device needs a stable ID before it can register with the rendezvous
// service). Generated once and persisted under Electron's userData
// directory so it survives restarts — unlike the auth tokens in
// renderer/src/App.tsx, this identity has nowhere else to come from, so
// it can't be left as an acknowledged gap the way token persistence was.
import { app } from "electron";
import { generateKeyPairSync, randomUUID } from "node:crypto";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import path from "node:path";

export interface DeviceIdentity {
  deviceId: string;
  publicKeyPem: string;
}

interface StoredIdentity extends DeviceIdentity {
  privateKeyPem: string;
}

function identityPath(): string {
  return path.join(app.getPath("userData"), "device-identity.json");
}

// Ed25519 via Node's own vetted crypto module — not hand-rolled, same
// "wrap, don't reinvent" rule as every cryptographic primitive elsewhere
// in this project. The private key stays on disk for whenever this
// identity needs to sign something (not yet — RegisterDevice today only
// takes the public key); it deliberately isn't sent anywhere.
function generateIdentity(): StoredIdentity {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  return {
    deviceId: randomUUID(),
    publicKeyPem: publicKey.export({ type: "spki", format: "pem" }).toString(),
    privateKeyPem: privateKey.export({ type: "pkcs8", format: "pem" }).toString(),
  };
}

let cached: DeviceIdentity | null = null;

export function loadOrCreateDeviceIdentity(): DeviceIdentity {
  if (cached) return cached;

  const file = identityPath();
  if (existsSync(file)) {
    const stored = JSON.parse(readFileSync(file, "utf8")) as StoredIdentity;
    cached = { deviceId: stored.deviceId, publicKeyPem: stored.publicKeyPem };
    return cached;
  }

  const identity = generateIdentity();
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, JSON.stringify(identity, null, 2), { mode: 0o600 });
  cached = { deviceId: identity.deviceId, publicKeyPem: identity.publicKeyPem };
  return cached;
}
