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

declare global {
  interface Window {
    nexdesk: {
      version: string;
      auth: {
        register(email: string, password: string): Promise<AuthResult>;
        login(email: string, password: string): Promise<AuthResult>;
        loginTwoFactor(pendingToken: string, code: string): Promise<AuthResult>;
      };
    };
  }
}
