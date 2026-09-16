// Talks to the Go backend's HTTP auth API (backend/internal/api/api.go).
// Framework-free on purpose: it only needs `fetch` (global since Node 18,
// which every supported Electron version bundles), so it can be exercised
// directly from a plain Node script against a real backend without
// spinning up Electron at all — see main/authClient.manual-check.mjs.
//
// This lives in the main process, not the renderer: keeping the backend
// URL and the raw network call in the trusted process mirrors why
// nodeIntegration is off in the renderer (main/index.ts) — the renderer
// only ever gets back parsed results via IPC, never a fetch primitive of
// its own.

export interface AuthResult {
  ok: boolean;
  status: number;
  body: unknown;
}

export interface BackendConfig {
  baseUrl: string;
}

async function postJSON(baseUrl: string, path: string, payload: unknown): Promise<AuthResult> {
  const res = await fetch(`${baseUrl}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await res.json().catch(() => ({}));
  return { ok: res.ok, status: res.status, body };
}

export function register(cfg: BackendConfig, email: string, password: string): Promise<AuthResult> {
  return postJSON(cfg.baseUrl, "/auth/register", { email, password });
}

export function login(cfg: BackendConfig, email: string, password: string): Promise<AuthResult> {
  return postJSON(cfg.baseUrl, "/auth/login", { email, password });
}

export function loginTwoFactor(cfg: BackendConfig, pendingToken: string, code: string): Promise<AuthResult> {
  return postJSON(cfg.baseUrl, "/auth/login/2fa", { pending_token: pendingToken, code });
}

export function refresh(cfg: BackendConfig, refreshToken: string): Promise<AuthResult> {
  return postJSON(cfg.baseUrl, "/auth/refresh", { refresh_token: refreshToken });
}
