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
  ephemeralPrivateKeyJwk: JsonWebKey;
  createdAt: number;
}

export interface TunnelRequest {
  id: string;
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: string;
  isBinary?: boolean;
}

export interface TunnelResponse {
  id: string;
  status: number;
  headers: Record<string, string>;
  body?: string;
  isBinary?: boolean;
}

export interface TunnelMessage {
  type: 'request' | 'response' | 'ping' | 'pong' | 'ws_open' | 'ws_data' | 'ws_close';
  data: TunnelRequest | TunnelResponse | WsOpen | WsData | WsClose;
}

export interface WsOpen {
  id: string;
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: string;
  isBinary?: boolean;
}

export interface WsData {
  id: string;
  data: string;
  isBinary: boolean;
}

export interface WsClose {
  id: string;
  code?: number;
  reason?: string;
}
