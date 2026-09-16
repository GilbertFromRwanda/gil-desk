// Device ID/connection screen (planner task E-07) plus session status
// (E-08) and connection errors (E-09). Shown once logged in. Talks to the
// backend's gRPC RendezvousService only through window.nexdesk.device
// (preload/index.ts -> IPC -> main/rendezvousClient.ts) — same
// no-network-primitives-in-the-renderer posture as the auth screen.
import { useEffect, useState } from "react";

type RegisterStatus = "idle" | "checking" | "registered" | "error";
type SessionRequestStatus = "idle" | "requesting" | "authorized" | "denied" | "error";

export default function ConnectScreen({
  accessToken,
  email,
  onLogOut,
}: {
  accessToken: string;
  email: string;
  onLogOut: () => void;
}) {
  const [deviceId, setDeviceId] = useState("");
  const [registerStatus, setRegisterStatus] = useState<RegisterStatus>("idle");
  const [registerMessage, setRegisterMessage] = useState("");
  const [targetDeviceId, setTargetDeviceId] = useState("");
  const [sessionStatus, setSessionStatus] = useState<SessionRequestStatus>("idle");
  const [sessionMessage, setSessionMessage] = useState("");
  const [sessionToken, setSessionToken] = useState("");

  useEffect(() => {
    window.nexdesk.device.getIdentity().then((identity) => setDeviceId(identity.deviceId));
  }, []);

  async function handleRegister() {
    setRegisterStatus("checking");
    setRegisterMessage("");
    const result = await window.nexdesk.device.register(accessToken);
    setRegisterStatus(result.ok ? "registered" : "error");
    setRegisterMessage(result.message);
  }

  async function handleRequestSession(e: React.FormEvent) {
    e.preventDefault();
    setSessionStatus("requesting");
    setSessionMessage("");
    setSessionToken("");
    const result = await window.nexdesk.device.requestSession(accessToken, targetDeviceId);
    if (!result.ok) {
      setSessionStatus("error");
      setSessionMessage(result.message);
      return;
    }
    if (!result.authorized) {
      setSessionStatus("denied");
      setSessionMessage(result.message);
      return;
    }
    setSessionStatus("authorized");
    setSessionToken(result.sessionToken);
    setSessionMessage(`authorized for ${result.expiresInSeconds}s`);
  }

  return (
    <div style={{ fontFamily: "sans-serif", padding: 24 }}>
      <h1>NexDesk</h1>
      <p>Logged in as <strong>{email}</strong>.</p>
      <p style={{ fontFamily: "monospace", fontSize: 12, wordBreak: "break-all" }}>
        access token: {accessToken.slice(0, 24)}...
      </p>
      <button onClick={onLogOut}>Log out</button>

      <hr style={{ margin: "24px 0" }} />

      <h2>This device</h2>
      <p style={{ fontFamily: "monospace", fontSize: 12, wordBreak: "break-all" }}>
        device ID: {deviceId || "generating..."}
      </p>
      <button onClick={handleRegister} disabled={!deviceId || registerStatus === "checking"}>
        Register this device
      </button>
      {registerStatus === "registered" && <p style={{ color: "green" }}>{registerMessage}</p>}
      {registerStatus === "error" && <p style={{ color: "crimson" }}>{registerMessage}</p>}

      <h2>Connect to a peer</h2>
      <form onSubmit={handleRequestSession}>
        <input
          value={targetDeviceId}
          onChange={(e) => setTargetDeviceId(e.target.value)}
          placeholder="target device ID"
          required
        />
        <button type="submit" disabled={sessionStatus === "requesting"}>Request session</button>
      </form>
      {sessionStatus === "authorized" && (
        <div style={{ color: "green" }}>
          <p>{sessionMessage}</p>
          <p style={{ fontFamily: "monospace", fontSize: 12, wordBreak: "break-all" }}>
            session token: {sessionToken.slice(0, 24)}...
          </p>
        </div>
      )}
      {sessionStatus === "denied" && <p style={{ color: "darkorange" }}>{sessionMessage}</p>}
      {sessionStatus === "error" && <p style={{ color: "crimson" }}>{sessionMessage}</p>}
    </div>
  );
}
