// Login / session shell (planner tasks E-06 Login, E-08 Session status,
// E-09 Connection errors). Talks to the real backend only through
// window.nexdesk.auth (preload/index.ts -> IPC -> main/authClient.ts) —
// the renderer itself never holds a network primitive, per
// contextIsolation/nodeIntegration in main/index.ts.
//
// Known gap, stated rather than hidden: tokens below live only in React
// state, so they're lost on app restart. Persisting them needs an
// OS-backed secure store (Electron's `safeStorage`, keychain/DPAPI-backed)
// wired through its own IPC call — not implemented yet.
import { useState } from "react";
import ConnectScreen from "./ConnectScreen";

type View = "auth" | "twoFactor" | "session";
type Mode = "login" | "register";

export default function App() {
  const [view, setView] = useState<View>("auth");
  const [mode, setMode] = useState<Mode>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [pendingToken, setPendingToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [accessToken, setAccessToken] = useState("");
  const [sessionEmail, setSessionEmail] = useState("");

  async function handleAuthSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const result = mode === "login"
        ? await window.nexdesk.auth.login(email, password)
        : await window.nexdesk.auth.register(email, password);

      if (!result.ok) {
        setError(result.body.error ?? `request failed (${result.status})`);
        return;
      }
      if (result.body.two_factor_required && result.body.pending_token) {
        setPendingToken(result.body.pending_token);
        setView("twoFactor");
        return;
      }
      if (result.body.access_token) {
        setAccessToken(result.body.access_token);
        setSessionEmail(email);
        setView("session");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "could not reach backend");
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
        setError(result.body.error ?? `request failed (${result.status})`);
        return;
      }
      if (result.body.access_token) {
        setAccessToken(result.body.access_token);
        setSessionEmail(email);
        setView("session");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "could not reach backend");
    } finally {
      setBusy(false);
    }
  }

  function logOut() {
    setAccessToken("");
    setSessionEmail("");
    setPassword("");
    setCode("");
    setPendingToken("");
    setError("");
    setView("auth");
  }

  if (view === "session") {
    return <ConnectScreen accessToken={accessToken} email={sessionEmail} onLogOut={logOut} />;
  }

  if (view === "twoFactor") {
    return (
      <div style={{ fontFamily: "sans-serif", padding: 24 }}>
        <h1>NexDesk</h1>
        <form onSubmit={handleTwoFactorSubmit}>
          <p>Enter the 6-digit code from your authenticator app.</p>
          <input
            autoFocus
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="123456"
            inputMode="numeric"
            maxLength={6}
          />
          <button type="submit" disabled={busy}>Verify</button>
        </form>
        {error && <p style={{ color: "crimson" }}>{error}</p>}
      </div>
    );
  }

  return (
    <div style={{ fontFamily: "sans-serif", padding: 24 }}>
      <h1>NexDesk</h1>
      <form onSubmit={handleAuthSubmit}>
        <div>
          <input
            autoFocus
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="email"
            required
          />
        </div>
        <div>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="password"
            minLength={8}
            required
          />
        </div>
        <button type="submit" disabled={busy}>
          {mode === "login" ? "Log in" : "Register"}
        </button>
      </form>
      <p>
        {mode === "login" ? "Need an account? " : "Already have an account? "}
        <a
          href="#"
          onClick={(e) => {
            e.preventDefault();
            setError("");
            setMode(mode === "login" ? "register" : "login");
          }}
        >
          {mode === "login" ? "Register" : "Log in"}
        </a>
      </p>
      {error && <p style={{ color: "crimson" }}>{error}</p>}
    </div>
  );
}
