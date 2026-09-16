// Electron main process entrypoint (planner tasks E-01..E-05).
import { app, BrowserWindow, ipcMain } from "electron";
import path from "node:path";
import * as authClient from "./authClient";

const backendConfig = { baseUrl: process.env.NEXDESK_BACKEND_URL ?? "http://localhost:8080" };

function createWindow(): void {
  const win = new BrowserWindow({
    width: 1024,
    height: 720,
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  win.loadURL(
    process.env.NEXDESK_RENDERER_URL ?? `file://${path.join(__dirname, "../renderer/index.html")}`
  );
}

// Every handler below just forwards to authClient and returns its result
// verbatim — the renderer (via preload's contextBridge, see
// preload/index.ts) gets the same {ok, status, body} shape the backend
// actually returned, so error messages the Go server wrote (e.g. "invalid
// email or password") reach the UI unchanged instead of being redecided
// here.
ipcMain.handle("auth:register", (_event, email: string, password: string) =>
  authClient.register(backendConfig, email, password)
);
ipcMain.handle("auth:login", (_event, email: string, password: string) =>
  authClient.login(backendConfig, email, password)
);
ipcMain.handle("auth:loginTwoFactor", (_event, pendingToken: string, code: string) =>
  authClient.loginTwoFactor(backendConfig, pendingToken, code)
);

app.whenReady().then(createWindow);

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow();
  }
});
