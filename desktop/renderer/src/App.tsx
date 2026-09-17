// App shell. There is no login screen: an install's own account is
// auto-generated and managed entirely by the main process
// (main/accountIdentity.ts) so that RegisterDevice/RequestSession still
// ride on the backend's real, unchanged account/JWT system (G-13..G-19)
// without asking anyone to create one or remember a password — the
// renderer never sees a token at all now, let alone a login form.
import ConnectScreen from "./ConnectScreen";

export default function App() {
  return <ConnectScreen />;
}
