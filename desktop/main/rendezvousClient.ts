// gRPC client for the backend's RendezvousService (planner tasks
// E-07/G-08..G-12 meeting in the middle: the desktop app's "device
// ID/connection screen" needs to actually call RegisterDevice and
// RequestSession, not just display static text).
//
// Loaded via @grpc/proto-loader directly from the shared .proto file
// instead of generated stubs — there's no JS leg of the existing
// Docker-based codegen pipeline (scripts/gen-proto.sh only emits Rust and
// Go), and proto-loader's reflection-based approach avoids standing one
// up just for this. The backend's gRPC server is plaintext (see
// cmd/server/main.go's grpc.NewServer() with no transport credentials) —
// fine for same-machine dev, but a real deployment needs TLS here too;
// tracked as a gap alongside the other dev-only shortcuts in this repo
// (ephemeral signing keys, unencrypted TOTP secrets at rest).
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import path from "node:path";

const PROTO_PATH = path.join(__dirname, "../../../proto/nexdesk/v1/rendezvous.proto");

export interface RegisterDeviceResult {
  ok: boolean;
  message: string;
}

export interface RequestSessionResult {
  ok: boolean;
  authorized: boolean;
  sessionToken: string;
  expiresInSeconds: number;
  message: string;
}

interface RendezvousClient extends grpc.Client {
  registerDevice(
    request: { device_id: string; public_key: string },
    metadata: grpc.Metadata,
    callback: (err: grpc.ServiceError | null, response: { accepted: boolean }) => void
  ): void;
  requestSession(
    request: { requester_device_id: string; target_device_id: string },
    metadata: grpc.Metadata,
    callback: (
      err: grpc.ServiceError | null,
      response: { authorized: boolean; session_token: string; expires_in_seconds: number }
    ) => void
  ): void;
}

let client: RendezvousClient | null = null;

function getClient(grpcAddr: string): RendezvousClient {
  if (client) return client;

  const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
    keepCase: true,
    longs: Number,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  const proto = grpc.loadPackageDefinition(packageDefinition) as unknown as {
    nexdesk: { v1: { RendezvousService: new (addr: string, creds: grpc.ChannelCredentials) => RendezvousClient } };
  };
  client = new proto.nexdesk.v1.RendezvousService(grpcAddr, grpc.credentials.createInsecure());
  return client;
}

function authMetadata(accessToken: string): grpc.Metadata {
  const metadata = new grpc.Metadata();
  metadata.set("authorization", `Bearer ${accessToken}`);
  return metadata;
}

function describeError(err: grpc.ServiceError): string {
  switch (err.code) {
    case grpc.status.UNAUTHENTICATED:
      return "session expired, please log in again";
    case grpc.status.PERMISSION_DENIED:
      return "you don't own this device";
    case grpc.status.UNAVAILABLE:
      return "could not reach the backend";
    default:
      return err.message;
  }
}

export function registerDevice(
  grpcAddr: string,
  accessToken: string,
  deviceId: string,
  publicKeyPem: string
): Promise<RegisterDeviceResult> {
  return new Promise((resolve) => {
    getClient(grpcAddr).registerDevice(
      { device_id: deviceId, public_key: publicKeyPem },
      authMetadata(accessToken),
      (err, response) => {
        if (err) {
          resolve({ ok: false, message: describeError(err) });
          return;
        }
        resolve({ ok: response.accepted, message: response.accepted ? "registered" : "rejected" });
      }
    );
  });
}

export function requestSession(
  grpcAddr: string,
  accessToken: string,
  requesterDeviceId: string,
  targetDeviceId: string
): Promise<RequestSessionResult> {
  return new Promise((resolve) => {
    getClient(grpcAddr).requestSession(
      { requester_device_id: requesterDeviceId, target_device_id: targetDeviceId },
      authMetadata(accessToken),
      (err, response) => {
        if (err) {
          resolve({ ok: false, authorized: false, sessionToken: "", expiresInSeconds: 0, message: describeError(err) });
          return;
        }
        resolve({
          ok: true,
          authorized: response.authorized,
          sessionToken: response.session_token,
          expiresInSeconds: response.expires_in_seconds,
          message: response.authorized ? "authorized" : "not authorized for this device",
        });
      }
    );
  });
}
