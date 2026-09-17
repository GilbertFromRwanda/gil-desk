// Auto-bootstrapped backend account, invisible to the user. The
// backend's G-17 device-ownership rule means RegisterDevice/RequestSession
// need a real authenticated account behind them — but there's nothing
// for a person to gain from picking that account's email and password
// themselves, so this install generates and remembers one on its own,
// the same way deviceIdentity.ts generates and remembers this device's
// keypair. A real account, with a real bcrypt-hashed password
// server-side (see backend/internal/auth/account.go) — just never
// manually entered or seen.
//
// Stated scope: this replaces E-06's original "Login" screen for this
// pass (the user asked for the app to open straight to its device ID,
// not a login form). The account system itself, 2FA included, is
// untouched server-side — there's just no UI in front of it right now
// for a person to manage their own account with instead of the
// auto-generated one.
import { app } from "electron";
import { randomBytes } from "node:crypto";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import * as authClient from "./authClient";

interface StoredAccount {
  email: string;
  password: string;
}

interface TokenResponseBody {
  access_token?: string;
  error?: string;
}

function accountPath(): string {
  return path.join(app.getPath("userData"), "account-identity.json");
}

function loadOrCreateAccountCredentials(): StoredAccount {
  const file = accountPath();
  if (existsSync(file)) {
    return JSON.parse(readFileSync(file, "utf8")) as StoredAccount;
  }
  const account: StoredAccount = {
    email: `device-${randomBytes(8).toString("hex")}@nexdesk.local`,
    // 24 random bytes comfortably clears the backend's 8-character
    // minimum (account.go) with real entropy, not a placeholder value.
    password: randomBytes(24).toString("base64url"),
  };
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, JSON.stringify(account, null, 2), { mode: 0o600 });
  return account;
}

let cachedAccessToken: string | null = null;

// Logs in (every launch after the first) or registers (first launch)
// with this install's own auto-generated account, and returns a real
// access token — the same shape a person logging in by hand would get,
// just without anyone typing anything. Cached in memory only for this
// process's lifetime: a fresh token is fetched each start rather than
// persisting one, since the login round trip is cheap and this avoids
// needing refresh-token rotation bookkeeping for a credential nothing
// ever reads back.
export async function bootstrapAccessToken(backendConfig: { baseUrl: string }): Promise<string> {
  if (cachedAccessToken) return cachedAccessToken;

  const { email, password } = loadOrCreateAccountCredentials();

  const loginResult = await authClient.login(backendConfig, email, password);
  const loginBody = loginResult.body as TokenResponseBody;
  if (loginResult.ok && loginBody.access_token) {
    cachedAccessToken = loginBody.access_token;
    return cachedAccessToken;
  }

  // Falls through here only on the very first launch, when this
  // install's account doesn't exist on the backend yet.
  const registerResult = await authClient.register(backendConfig, email, password);
  const registerBody = registerResult.body as TokenResponseBody;
  if (!registerResult.ok || !registerBody.access_token) {
    throw new Error(`could not bootstrap this device's account: ${registerBody.error ?? "unknown error"}`);
  }
  cachedAccessToken = registerBody.access_token;
  return cachedAccessToken;
}
