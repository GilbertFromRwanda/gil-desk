// Device ID/connection screen (planner task E-07) plus session status
// (E-08) and connection errors (E-09) — the app's only real screen now
// that there's no login flow in front of it. Talks to the backend's
// gRPC RendezvousService only through window.nexdesk.device
// (preload/index.ts -> IPC -> main/rendezvousClient.ts); the access
// token that needs is entirely main-process-side now
// (main/accountIdentity.ts), so this component never sees one.
import { useEffect, useState } from "react";
import AccountPanel from "./AccountPanel";
import InputCapture from "./InputCapture";
import RelayChat from "./RelayChat";
import SessionTabs, { type FavoriteDevice, type RecentSession } from "./SessionTabs";

type RegisterStatus = "idle" | "registering" | "registered" | "error";
type SessionRequestStatus = "idle" | "requesting" | "authorized" | "denied" | "error";

// "123456789" -> "123 456 789" — a device ID exists so a person can read
// it off one screen and type it into another; grouping digits is the
// same trick phone numbers and TeamViewer/AnyDesk-style IDs use to make
// that actually practical. Purely a display concern — the ungrouped
// digits are still what's sent to the backend.
export function formatDeviceId(id: string): string {
  return id.replace(/(\d{3})(?=\d)/g, "$1 ");
}

const FAVORITES_KEY = "nexdesk.favorites";
const RECENT_KEY = "nexdesk.recentSessions";
const THEME_KEY = "nexdesk.theme";

function loadFromStorage<T>(key: string): T[] {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T[]) : [];
  } catch {
    return [];
  }
}

type Theme = "system" | "light" | "dark";

function loadTheme(): Theme {
  try {
    const raw = localStorage.getItem(THEME_KEY);
    return raw === "light" || raw === "dark" ? raw : "system";
  } catch {
    return "system";
  }
}

const THEME_LABEL: Record<Theme, string> = { system: "Auto", light: "Light", dark: "Dark" };
const THEME_ICON: Record<Theme, string> = { system: "◐", light: "☀", dark: "☽" };
const NEXT_THEME: Record<Theme, Theme> = { system: "light", light: "dark", dark: "system" };

export default function ConnectScreen() {
  const [deviceId, setDeviceId] = useState("");
  const [registerStatus, setRegisterStatus] = useState<RegisterStatus>("idle");
  const [registerMessage, setRegisterMessage] = useState("");
  const [targetDeviceId, setTargetDeviceId] = useState("");
  const [sessionStatus, setSessionStatus] = useState<SessionRequestStatus>("idle");
  const [sessionMessage, setSessionMessage] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [showAccountPanel, setShowAccountPanel] = useState(false);
  const [loggedInEmail, setLoggedInEmail] = useState<string | null>(null);
  const [favorites, setFavorites] = useState<FavoriteDevice[]>(() => loadFromStorage<FavoriteDevice>(FAVORITES_KEY));
  const [recentSessions, setRecentSessions] = useState<RecentSession[]>(() => loadFromStorage<RecentSession>(RECENT_KEY));
  const [theme, setTheme] = useState<Theme>(() => loadTheme());

  useEffect(() => {
    if (theme === "system") {
      document.documentElement.removeAttribute("data-theme");
    } else {
      document.documentElement.setAttribute("data-theme", theme);
    }
    try {
      localStorage.setItem(THEME_KEY, theme);
    } catch {
      // best-effort — theme choice just won't survive a restart
    }
  }, [theme]);

  useEffect(() => {
    try {
      localStorage.setItem(FAVORITES_KEY, JSON.stringify(favorites));
    } catch {
      // best-effort — favorites just won't survive a restart
    }
  }, [favorites]);

  useEffect(() => {
    try {
      localStorage.setItem(RECENT_KEY, JSON.stringify(recentSessions));
    } catch {
      // best-effort — history just won't survive a restart
    }
  }, [recentSessions]);

  useEffect(() => {
    window.nexdesk.device.getIdentity().then((identity) => setDeviceId(identity.deviceId));
  }, []);

  // Registers automatically as soon as this device's ID is known —
  // there's nothing for the user to decide here (unlike requesting a
  // session with a specific peer), so making them click a button just to
  // get to a usable ID would be friction with no payoff.
  useEffect(() => {
    if (!deviceId) return;
    let cancelled = false;
    setRegisterStatus("registering");
    window.nexdesk.device.register().then((result) => {
      if (cancelled) return;
      setRegisterStatus(result.ok ? "registered" : "error");
      setRegisterMessage(result.message);
    });
    return () => {
      cancelled = true;
    };
  }, [deviceId]);

  function pushRecentSession(id: string, status: RecentSession["status"]) {
    setRecentSessions((prev) => [{ deviceId: id, at: Date.now(), status }, ...prev.filter((s) => s.deviceId !== id)].slice(0, 8));
  }

  async function connectToDevice(rawId: string) {
    const id = rawId.replace(/\s/g, "");
    if (!id) return;
    setTargetDeviceId(id);
    setSessionStatus("requesting");
    setSessionMessage("");
    setSessionToken("");
    const result = await window.nexdesk.device.requestSession(id);
    if (!result.ok) {
      setSessionStatus("error");
      setSessionMessage(result.message);
      pushRecentSession(id, "error");
      return;
    }
    if (!result.authorized) {
      setSessionStatus("denied");
      setSessionMessage(result.message);
      pushRecentSession(id, "denied");
      return;
    }
    setSessionStatus("authorized");
    setSessionToken(result.sessionToken);
    setSessionMessage(`Authorized for ${result.expiresInSeconds}s`);
    pushRecentSession(id, "authorized");
  }

  function handleRequestSession(e: React.FormEvent) {
    e.preventDefault();
    connectToDevice(targetDeviceId);
  }

  function addFavorite(id: string, label: string) {
    setFavorites((prev) => (prev.some((f) => f.deviceId === id) ? prev : [...prev, { deviceId: id, label }]));
  }

  function removeFavorite(id: string) {
    setFavorites((prev) => prev.filter((f) => f.deviceId !== id));
  }

  return (
    <div className="app-shell">
      <div className="topbar">
        <div className="topbar-left">
          <div className="brand">
            <div className="brand-mark" aria-hidden>N</div>
            NexDesk
          </div>
          <form onSubmit={handleRequestSession} className="row quick-connect">
            <input
              type="text"
              inputMode="numeric"
              value={targetDeviceId}
              onChange={(e) => setTargetDeviceId(e.target.value)}
              placeholder="Enter partner ID"
              required
            />
            <button type="submit" className="btn-primary" style={{ width: "auto" }} disabled={sessionStatus === "requesting"}>
              {sessionStatus === "requesting" ? "Connecting..." : "Connect"}
            </button>
          </form>
        </div>
        {/* NexDesk works fully without an account — this install's own
            auto-generated one (main/accountIdentity.ts) is invisible on
            purpose. This opens a real, on-demand login/register panel
            (AccountPanel) for someone who explicitly wants one, e.g. for
            a future paid tier — never required to use the app. */}
        <div className="row" style={{ gap: 12, alignItems: "center" }}>
          <button
            type="button"
            className="btn-ghost theme-toggle"
            title={`Theme: ${THEME_LABEL[theme]} (click to change)`}
            onClick={() => setTheme(NEXT_THEME[theme])}
          >
            {THEME_ICON[theme]} {THEME_LABEL[theme]}
          </button>
          {loggedInEmail ? (
            <>
              <span className="text-muted mono">{loggedInEmail}</span>
              <button className="btn-ghost" onClick={() => setLoggedInEmail(null)}>Log out</button>
            </>
          ) : (
            !showAccountPanel && (
              <button className="btn-ghost" onClick={() => setShowAccountPanel(true)}>Log in</button>
            )
          )}
        </div>
      </div>

      {sessionStatus !== "idle" && sessionStatus !== "requesting" && sessionMessage && (
        <div
          className={
            "topbar-banner " +
            (sessionStatus === "authorized" ? "topbar-banner-success" : sessionStatus === "denied" ? "topbar-banner-warning" : "topbar-banner-danger")
          }
        >
          {sessionMessage}
        </div>
      )}

      {showAccountPanel && !loggedInEmail && (
        <div className="main-content" style={{ paddingBottom: 0 }}>
          <AccountPanel
            onClose={() => setShowAccountPanel(false)}
            onLoggedIn={(email) => setLoggedInEmail(email)}
          />
        </div>
      )}

      <div className="main-content">
        <div className="card">
          <p className="card-title">Your device ID</p>
          <div className="device-id-display">{deviceId ? formatDeviceId(deviceId) : "Generating..."}</div>
          <div className="text-muted">
            {registerStatus === "registering" && <span><span className="status-dot offline" />Connecting to NexDesk...</span>}
            {registerStatus === "registered" && <span><span className="status-dot online" />Ready to receive connections</span>}
            {registerStatus === "error" && <span className="alert alert-danger" style={{ display: "inline-block", marginTop: 0 }}>{registerMessage}</span>}
          </div>
        </div>

        <SessionTabs
          favorites={favorites}
          recentSessions={recentSessions}
          onAddFavorite={addFavorite}
          onRemoveFavorite={removeFavorite}
          onConnect={connectToDevice}
        />

        {sessionStatus === "authorized" && <RelayChat sessionToken={sessionToken} />}

        <details className="dev-tools">
          <summary>Developer tools</summary>
          <div className="dev-panel">
            <InputCapture />
          </div>
        </details>
      </div>
    </div>
  );
}
