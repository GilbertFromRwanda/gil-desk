// Real keyboard/mouse/scroll capture (planner tasks E-14/E-15/E-16).
// Every event here is a genuine DOM event from this focusable region, not
// a synthetic/injected one — each is sent to the main process
// (window.nexdesk.input.roundTrip -> IPC -> main/nativeBridge.ts) which
// encodes it through the real compiled Rust addon (core::input::InputEvent,
// the same format the wire protocol between two live NexDesk peers will
// eventually use) and decodes what it just produced, so what's shown
// below is what the native module actually returned — not a JS-side
// assumption that encoding worked.
//
// Stated scope: there's no live remote session to send these events over
// yet (Gate G3's "establish secure session" step needs NAT
// traversal/relay work that hasn't been built) — this proves the
// capture -> native-encode -> native-decode leg of the pipeline, which is
// what E-14/E-15/E-16 actually ask for.
import { useRef, useState } from "react";

interface LogEntry {
  id: number;
  summary: string;
  encodedHex: string;
}

const MOUSE_MOVE_THROTTLE_MS = 150;

export default function InputCapture() {
  const [log, setLog] = useState<LogEntry[]>([]);
  const nextId = useRef(0);
  const lastMouseMoveSentAt = useRef(0);

  function record(summary: string, encodedHex: string) {
    setLog((prev) => [{ id: nextId.current++, summary, encodedHex }, ...prev].slice(0, 8));
  }

  async function send(event: Parameters<typeof window.nexdesk.input.roundTrip>[0], summary: string) {
    const result = await window.nexdesk.input.roundTrip(event);
    record(summary, result.encodedHex);
  }

  return (
    <div>
      <p className="card-title">Input capture (E-14/E-15/E-16)</p>
      <p className="text-muted">
        Click into the box below, then type or move/click/scroll the mouse over it. Each real event
        is encoded and decoded through the native Rust addon.
      </p>
      <div
        className="capture-box"
        tabIndex={0}
        onKeyDown={(e) => {
          e.preventDefault();
          send({ kind: "keyDown", code: e.keyCode }, `keyDown "${e.key}" (code ${e.keyCode})`);
        }}
        onKeyUp={(e) => {
          e.preventDefault();
          send({ kind: "keyUp", code: e.keyCode }, `keyUp "${e.key}" (code ${e.keyCode})`);
        }}
        onMouseMove={(e) => {
          const now = performance.now();
          if (now - lastMouseMoveSentAt.current < MOUSE_MOVE_THROTTLE_MS) return;
          lastMouseMoveSentAt.current = now;
          const x = Math.round(e.nativeEvent.offsetX);
          const y = Math.round(e.nativeEvent.offsetY);
          send({ kind: "mouseMove", x, y }, `mouseMove (${x}, ${y})`);
        }}
        onMouseDown={(e) => {
          send({ kind: "mouseButton", button: e.button, down: true }, `mouseDown button ${e.button}`);
        }}
        onMouseUp={(e) => {
          send({ kind: "mouseButton", button: e.button, down: false }, `mouseUp button ${e.button}`);
        }}
        onWheel={(e) => {
          const dx = Math.round(e.deltaX);
          const dy = Math.round(e.deltaY);
          send({ kind: "scroll", dx, dy }, `scroll (${dx}, ${dy})`);
        }}
      >
        (focus here and interact)
      </div>
      <ul className="log-list">
        {log.map((entry) => (
          <li key={entry.id}>
            {entry.summary} → {entry.encodedHex}
          </li>
        ))}
      </ul>
    </div>
  );
}
