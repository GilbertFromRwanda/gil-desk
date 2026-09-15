// Preload bridge — explicit, minimal API surface exposed to the renderer
// (planner Section 21, "Rust ↔ Electron": minimize JS/native copies).
import { contextBridge } from "electron";

contextBridge.exposeInMainWorld("nexdesk", {
  version: "0.1.0",
});
