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
        register(accessToken: string): Promise<RegisterDeviceResult>;
        requestSession(accessToken: string, targetDeviceId: string): Promise<RequestSessionResult>;
      };
    };
  }
}
