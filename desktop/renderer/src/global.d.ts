export {};

interface AuthResult {
  ok: boolean;
  status: number;
  body: {
    error?: string;
    access_token?: string;
    refresh_token?: string;
    two_factor_required?: boolean;
    pending_token?: string;
    expires_in_seconds?: number;
  };
}

interface DeviceIdentity {
  deviceId: string;
  publicKeyPem: string;
}

interface RegisterDeviceResult {
  ok: boolean;
  message: string;
}

interface RequestSessionResult {
  ok: boolean;
  authorized: boolean;
  sessionToken: string;
  expiresInSeconds: number;
  message: string;
}

interface CapturedEvent {
  kind: "keyDown" | "keyUp" | "mouseMove" | "mouseButton" | "scroll";
  code?: number;
  x?: number;
  y?: number;
  button?: number;
  down?: boolean;
  dx?: number;
  dy?: number;
}

interface DecodedInputEvent {
  kind: string;
  code?: number;
  x?: number;
  y?: number;
  button?: number;
  down?: boolean;
  dx?: number;
  dy?: number;
}

interface InputRoundTripResult {
  encodedHex: string;
  decoded: DecodedInputEvent;
}

interface RelayConnectResult {
  sessionId: string;
  peerDeviceId: string;
}

declare global {
  interface Window {
    nexdesk: {
      version: string;
      auth: {
        register(email: string, password: string): Promise<AuthResult>;
        login(email: string, password: string): Promise<AuthResult>;
        loginTwoFactor(pendingToken: string, code: string): Promise<AuthResult>;
      };
      device: {
        getIdentity(): Promise<DeviceIdentity>;
        register(): Promise<RegisterDeviceResult>;
        requestSession(targetDeviceId: string): Promise<RequestSessionResult>;
      };
      input: {
        roundTrip(event: CapturedEvent): Promise<InputRoundTripResult>;
      };
      relay: {
        connect(sessionToken: string): Promise<RelayConnectResult>;
        send(sessionId: string, text: string): Promise<void>;
        recv(sessionId: string): Promise<string>;
      };
    };
  }
}
