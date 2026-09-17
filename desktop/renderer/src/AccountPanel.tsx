// On-demand account login/register (planner E-06), separate from the
// automatic per-device account main/accountIdentity.ts silently
// bootstraps for RegisterDevice/RequestSession — that one needs no
// human involvement at all. This is for someone who explicitly wants to
// log into (or create) a real account of their own, reachable only via
// ConnectScreen's "Log in" button — the app works fully without ever
// opening this panel.
import { useState } from "react";

type Mode = "login" | "register";
type Stage = "form" | "twoFactor";

export default function AccountPanel({
  onClose,
  onLoggedIn,
}: {
  onClose: () => void;
  onLoggedIn: (email: string) => void;
}) {
  const [stage, setStage] = useState<Stage>("form");
  const [mode, setMode] = useState<Mode>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [pendingToken, setPendingToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const result = mode === "login"
        ? await window.nexdesk.auth.login(email, password)
        : await window.nexdesk.auth.register(email, password);

      if (!result.ok) {
        setError(result.body.error ?? `Request failed (${result.status})`);
        return;
      }
      if (result.body.two_factor_required && result.body.pending_token) {
        setPendingToken(result.body.pending_token);
        setStage("twoFactor");
        return;
      }
      if (result.body.access_token) {
        onLoggedIn(email);
        onClose();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not reach the backend");
    } finally {
      setBusy(false);
    }
  }

  async function handleTwoFactorSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const result = await window.nexdesk.auth.loginTwoFactor(pendingToken, code);
      if (!result.ok) {
        setError(result.body.error ?? `Request failed (${result.status})`);
        return;
      }
      if (result.body.access_token) {
        onLoggedIn(email);
        onClose();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not reach the backend");
    } finally {
      setBusy(false);
    }
  }

  if (stage === "twoFactor") {
    return (
      <div className="card">
        <p className="card-title">Two-factor verification</p>
        <form onSubmit={handleTwoFactorSubmit}>
          <div className="field">
            <label htmlFor="account-totp">Authentication code</label>
            <input
              id="account-totp"
              autoFocus
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="123456"
              inputMode="numeric"
              maxLength={6}
              style={{ textAlign: "center", letterSpacing: "0.3em", fontSize: 18 }}
            />
          </div>
          <div className="row">
            <button type="submit" className="btn-primary" style={{ width: "auto" }} disabled={busy}>
              {busy ? "Verifying..." : "Verify"}
            </button>
            <button type="button" className="btn-secondary" style={{ width: "auto" }} onClick={onClose}>Cancel</button>
          </div>
        </form>
        {error && <div className="alert alert-danger">{error}</div>}
      </div>
    );
  }

  return (
    <div className="card">
      <p className="card-title">{mode === "login" ? "Log in" : "Create account"}</p>
      <form onSubmit={handleSubmit}>
        <div className="field">
          <label htmlFor="account-email">Email</label>
          <input
            id="account-email"
            autoFocus
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@example.com"
            required
          />
        </div>
        <div className="field">
          <label htmlFor="account-password">Password</label>
          <input
            id="account-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••"
            minLength={8}
            required
          />
        </div>
        <div className="row">
          <button type="submit" className="btn-primary" style={{ width: "auto" }} disabled={busy}>
            {busy ? "Please wait..." : mode === "login" ? "Log in" : "Create account"}
          </button>
          <button type="button" className="btn-secondary" style={{ width: "auto" }} onClick={onClose}>Cancel</button>
        </div>
      </form>
      <p className="text-muted" style={{ marginTop: 12, marginBottom: 0 }}>
        {mode === "login" ? "Need an account? " : "Already have an account? "}
        <a
          href="#"
          className="text-link"
          onClick={(e) => {
            e.preventDefault();
            setError("");
            setMode(mode === "login" ? "register" : "login");
          }}
        >
          {mode === "login" ? "Register" : "Log in"}
        </a>
      </p>
      {error && <div className="alert alert-danger">{error}</div>}
    </div>
  );
}
