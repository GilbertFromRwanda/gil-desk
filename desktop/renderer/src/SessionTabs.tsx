// Below-the-fold connection organizer: saved peers, connection history,
// and two honestly-empty placeholders for features the backend doesn't
// support yet. Favorites/Recent sessions are real and persisted
// per-install via localStorage (there's no backend concept of either —
// RendezvousService only knows RegisterDevice/RequestSession, see
// proto/nexdesk/v1/rendezvous.proto). Discovered (LAN auto-discovery) and
// Invitations (inbound connection requests) have no data source at all
// yet, so those tabs say so rather than faking content.
import { useState } from "react";
import { formatDeviceId } from "./ConnectScreen";

export type FavoriteDevice = { deviceId: string; label: string };
export type RecentSession = { deviceId: string; at: number; status: "authorized" | "denied" | "error" };

type Tab = "favorites" | "recent" | "discovered" | "invitations";

export default function SessionTabs({
  favorites,
  recentSessions,
  onAddFavorite,
  onRemoveFavorite,
  onConnect,
}: {
  favorites: FavoriteDevice[];
  recentSessions: RecentSession[];
  onAddFavorite: (deviceId: string, label: string) => void;
  onRemoveFavorite: (deviceId: string) => void;
  onConnect: (deviceId: string) => void;
}) {
  const [tab, setTab] = useState<Tab>("favorites");
  const [newId, setNewId] = useState("");
  const [newLabel, setNewLabel] = useState("");

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    const id = newId.replace(/\s/g, "");
    if (!id) return;
    onAddFavorite(id, newLabel.trim() || id);
    setNewId("");
    setNewLabel("");
  }

  return (
    <div className="card">
      <div className="tab-bar">
        {(
          [
            ["favorites", "Favorites"],
            ["recent", "Recent sessions"],
            ["discovered", "Discovered"],
            ["invitations", "Invitations"],
          ] as [Tab, string][]
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            className={tab === id ? "tab-button active" : "tab-button"}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="tab-panel">
        {tab === "favorites" && (
          <>
            <form onSubmit={handleAdd} className="row" style={{ marginBottom: "var(--space-4)" }}>
              <input
                type="text"
                inputMode="numeric"
                value={newId}
                onChange={(e) => setNewId(e.target.value)}
                placeholder="Device ID"
              />
              <input
                type="text"
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                placeholder="Label (optional)"
              />
              <button type="submit" className="btn-secondary" style={{ width: "auto" }}>
                Add
              </button>
            </form>
            {favorites.length === 0 ? (
              <p className="empty-state">No favorites yet. Save a device ID above to connect to it in one click.</p>
            ) : (
              <ul className="list-rows">
                {favorites.map((f) => (
                  <li key={f.deviceId} className="list-row">
                    <div>
                      <div>{f.label}</div>
                      <div className="text-muted mono">{formatDeviceId(f.deviceId)}</div>
                    </div>
                    <div className="row">
                      <button className="btn-secondary" style={{ width: "auto" }} onClick={() => onConnect(f.deviceId)}>
                        Connect
                      </button>
                      <button className="btn-ghost" onClick={() => onRemoveFavorite(f.deviceId)}>
                        Remove
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}

        {tab === "recent" && (
          recentSessions.length === 0 ? (
            <p className="empty-state">No recent sessions yet — connections you make will show up here.</p>
          ) : (
            <ul className="list-rows">
              {recentSessions.map((s) => (
                <li key={`${s.deviceId}-${s.at}`} className="list-row">
                  <div>
                    <div className="mono">{formatDeviceId(s.deviceId)}</div>
                    <div className="text-muted">{new Date(s.at).toLocaleString()}</div>
                  </div>
                  <div className="row" style={{ alignItems: "center" }}>
                    <span className={`badge badge-${s.status}`}>{s.status}</span>
                    <button className="btn-secondary" style={{ width: "auto" }} onClick={() => onConnect(s.deviceId)}>
                      Connect again
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )
        )}

        {tab === "discovered" && (
          <p className="empty-state">
            Automatic discovery of devices on this network isn't built yet — for now, connect using a device ID directly.
          </p>
        )}

        {tab === "invitations" && (
          <p className="empty-state">
            There's no invitation system yet — share your device ID with the other person so they can connect to you.
          </p>
        )}
      </div>
    </div>
  );
}
