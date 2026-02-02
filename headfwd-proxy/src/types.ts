// HeadFwd Proxy Types

export interface Env {
  TUNNELS: DurableObjectNamespace;
  REGISTRY?: KVNamespace;
  TUNNEL_SECRET: string;
}

export interface TunnelRegistration {
  publicKey: string;
  secret: string;
  createdAt: number;
}

export interface ChallengeData {
  publicKey: string;
  nonce: string;
  createdAt: number;
}

export interface TunnelRequest {
  id: string;
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: string;
}

export interface TunnelResponse {
  id: string;
  status: number;
  headers: Record<string, string>;
  body?: string;
}

export interface TunnelMessage {
  type: 'request' | 'response' | 'ping' | 'pong';
  data: TunnelRequest | TunnelResponse;
}

