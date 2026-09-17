// Per-install device identity (planner task E-07's prerequisite: a
// device needs a stable ID before it can register with the rendezvous
// service). Generated once and persisted under Electron's userData
// directory so it survives restarts — unlike the auth tokens in
// renderer/src/App.tsx, this identity has nowhere else to come from, so
// it can't be left as an acknowledged gap the way token persistence was.
import { app } from "electron";
import { generateKeyPairSync, randomInt } from "node:crypto";
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

// A 9-digit number, not a UUID — a real product decision, not a default:
// a device ID exists so a *person* can read it off one screen and type
// it into another (TeamViewer/AnyDesk/Chrome Remote Desktop all use
// short numeric IDs for exactly this reason), and
// "6d48494d-4094-4d15-a79e-19cb22338666" fails that job completely.
// Generated with node:crypto's randomInt (a real CSPRNG, not Math.random)
// — same "don't hand-roll randomness" rule as every other primitive in
// this project. There's no central ID-issuing authority to guarantee
// uniqueness the way a real TeamViewer-scale service would; at this
// project's scale, the ~900 million possible values make a collision
// vanishingly unlikely, and if one ever happened, RegisterDevice's
// existing ownership check (registry.go's ON CONFLICT ... WHERE
// owner_user_id = EXCLUDED.owner_user_id) would surface it as a real,
// visible PermissionDenied rather than silently misrouting a session.
function generateDeviceId(): string {
  return String(randomInt(100_000_000, 1_000_000_000));
}

// Ed25519 via Node's own vetted crypto module — not hand-rolled, same
// "wrap, don't reinvent" rule as every cryptographic primitive elsewhere
// in this project. The private key stays on disk for whenever this
// identity needs to sign something (not yet — RegisterDevice today only
// takes the public key); it deliberately isn't sent anywhere.
function generateIdentity(): StoredIdentity {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  return {
    deviceId: generateDeviceId(),
    publicKeyPem: publicKey.export({ type: "spki", format: "pem" }).toString(),
    privateKeyPem: privateKey.export({ type: "pkcs8", format: "pem" }).toString(),
  };
}

const NUMERIC_DEVICE_ID = /^\d{9}$/;

let cached: DeviceIdentity | null = null;

export function loadOrCreateDeviceIdentity(): DeviceIdentity {
  if (cached) return cached;

  const file = identityPath();
  if (existsSync(file)) {
    const stored = JSON.parse(readFileSync(file, "utf8")) as StoredIdentity;
    // Installs from before device IDs became numeric persisted a UUID —
    // regenerate rather than keep serving one, since a UUID can't be
    // read-and-typed the way this ID format exists to support. This
    // only matters for installs that ran the app before this change (at
    // this project's stage, that's dev/test environments, not a real
    // user base with a server-side identity to migrate).
    if (NUMERIC_DEVICE_ID.test(stored.deviceId)) {
      cached = { deviceId: stored.deviceId, publicKeyPem: stored.publicKeyPem };
      return cached;
    }
  }

  const identity = generateIdentity();
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, JSON.stringify(identity, null, 2), { mode: 0o600 });
  cached = { deviceId: identity.deviceId, publicKeyPem: identity.publicKeyPem };
  return cached;
}
