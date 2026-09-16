// Turns a RequestSession-issued token into an actual live connection
// (planner Gate G3's "establish secure session" step, and a real E-08
// session status) — shown once ConnectScreen's "Request session" is
// authorized. Talks to the relay only through window.nexdesk.relay
// (preload/index.ts -> IPC -> main/index.ts's relay:* handlers ->
// main/nativeBridge.ts's RelaySession, backed by a real Rust
// rendezvous::relay::RelayConnection) — the renderer still never touches
// a network or native primitive directly.
//
// Stated scope: messages are sent/received as opaque UTF-8 text over the
// relay's raw byte channel, not through the real end-to-end session
// protocol (SessionHello was already exchanged by the time this
// component gets a sessionId — see connect_and_handshake — but frame
// encoding beyond that, encryption, etc. is the CLI binaries'/future
// work's concern, not reimplemented here). A real host process (e.g.
// `host --relay ...`) will also periodically send its own "PING"
// heartbeat text, which shows up in the log below like any other
// message — that's expected, not a bug.
import { useEffect, useRef, useState } from "react";

type ConnectStatus = "idle" | "connecting" | "connected" | "error";

interface LogEntry {
  id: number;
  direction: "sent" | "received";
  text: string;
}

export default function RelayChat({ sessionToken }: { sessionToken: string }) {
  const [status, setStatus] = useState<ConnectStatus>("idle");
  const [errorMessage, setErrorMessage] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [peerDeviceId, setPeerDeviceId] = useState("");
  const [draft, setDraft] = useState("");
  const [log, setLog] = useState<LogEntry[]>([]);
  const nextId = useRef(0);
  const stopped = useRef(false);

  useEffect(() => {
    return () => {
      stopped.current = true;
    };
  }, []);

  function record(direction: LogEntry["direction"], text: string) {
    setLog((prev) => [...prev, { id: nextId.current++, direction, text }]);
  }

  async function receiveLoop(id: string) {
    while (!stopped.current) {
      try {
        const text = await window.nexdesk.relay.recv(id);
        record("received", text);
      } catch {
        break; // peer disconnected or the relay stream ended
      }
    }
  }

  async function handleConnect() {
    setStatus("connecting");
    setErrorMessage("");
    try {
      const result = await window.nexdesk.relay.connect(sessionToken);
      setSessionId(result.sessionId);
      setPeerDeviceId(result.peerDeviceId);
      setStatus("connected");
      receiveLoop(result.sessionId);
    } catch (err) {
      setStatus("error");
      setErrorMessage(err instanceof Error ? err.message : "could not connect through the relay");
    }
  }

  async function handleSend(e: React.FormEvent) {
    e.preventDefault();
    if (!draft) return;
    const text = draft;
    setDraft("");
    try {
      await window.nexdesk.relay.send(sessionId, text);
      record("sent", text);
    } catch (err) {
      setStatus("error");
      setErrorMessage(err instanceof Error ? err.message : "send failed");
    }
  }

  return (
    <div>
      <h2>Live session (Gate G3)</h2>
      {status === "idle" && <button onClick={handleConnect}>Connect through relay</button>}
      {status === "connecting" && <p>connecting...</p>}
      {status === "error" && <p style={{ color: "crimson" }}>{errorMessage}</p>}
      {status === "connected" && (
        <div>
          <p style={{ color: "green" }}>Connected to peer <strong>{peerDeviceId}</strong>.</p>
          <form onSubmit={handleSend}>
            <input value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="message" />
            <button type="submit">Send</button>
          </form>
          <ul style={{ fontFamily: "monospace", fontSize: 12, listStyle: "none", padding: 0 }}>
            {log.map((entry) => (
              <li key={entry.id}>
                {entry.direction === "sent" ? "-> " : "<- "}
                {entry.text}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
