// Durable Object: One per Headscale instance
// Holds persistent WebSocket tunnel from Headscale sidecar

import type { Env, TunnelRequest, TunnelResponse, TunnelMessage, WsOpen, WsData, WsClose } from './types';

export class HeadscaleTunnel implements DurableObject {
  private state: DurableObjectState;
  private env: Env;
  private tunnel: WebSocket | null = null;
  private pendingRequests: Map<string, { resolve: Function; reject: Function; timeout: NodeJS.Timeout }> = new Map();
  private wsConnections: Map<string, WebSocket> = new Map();
  
  constructor(state: DurableObjectState, env: Env) {
    this.state = state;
    this.env = env;
  }
  
  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    
    // Headscale sidecar connecting
    if (url.pathname === '/tunnel') {
      return this.acceptTunnel(request);
    }
    
    // Client request to forward
    return this.forwardToHeadscale(request);
  }
  
  /**
   * Accept tunnel connection from Headscale sidecar
   */
  private async acceptTunnel(request: Request): Promise<Response> {
    // Verify auth
    const url = new URL(request.url);
    const auth = url.searchParams.get('auth');
    const hostname = url.hostname;
    const fingerprint = extractFingerprint(hostname);

    if (!auth) {
      return new Response('Unauthorized', { status: 401 });
    }

    if (fingerprint && this.env.REGISTRY) {
      const registration = await this.env.REGISTRY.get(`tunnel:${fingerprint}`);
      if (!registration) {
        return new Response('Unauthorized', { status: 401 });
      }
      const parsed = JSON.parse(registration) as { secret?: string };
      if (!parsed.secret || parsed.secret !== auth) {
        return new Response('Unauthorized', { status: 401 });
      }
    } else {
      // Local dev fallback
      if (auth !== this.env.TUNNEL_SECRET) {
        return new Response('Unauthorized', { status: 401 });
      }
    }
    
    // Upgrade to WebSocket
    const upgradeHeader = request.headers.get('Upgrade');
    const wsKey = request.headers.get('Sec-WebSocket-Key');
    if (upgradeHeader !== 'websocket' && !wsKey) {
      return new Response('Expected WebSocket', { status: 426 });
    }
    
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    
    server.accept();
    this.tunnel = server;
    
    // Handle messages from Headscale
    server.addEventListener('message', (event) => {
      this.handleTunnelMessage(event.data);
    });
    
    server.addEventListener('close', () => {
      this.tunnel = null;
      // Reject all pending requests
      for (const [id, pending] of this.pendingRequests) {
        clearTimeout(pending.timeout);
        pending.reject(new Error('Tunnel closed'));
      }
      this.pendingRequests.clear();
    });
    
    server.addEventListener('error', (event) => {
      console.error('Tunnel error:', event);
    });
    
    return new Response(null, {
      status: 101,
      webSocket: client,
    });
  }
  
  /**
   * Forward client request through tunnel to Headscale
   */
  private async forwardToHeadscale(request: Request): Promise<Response> {
    // WebSocket upgrade
    const upgradeHeader = request.headers.get('Upgrade');
    const wsKey = request.headers.get('Sec-WebSocket-Key');
    if ((upgradeHeader && upgradeHeader.toLowerCase() === 'websocket') || wsKey) {
      if (request.url.includes('/ts2021')) {
        console.log('TS2021 WS upgrade', {
          host: request.headers.get('Host'),
          upgrade: upgradeHeader,
          wsKey: wsKey ? 'present' : 'missing',
          connection: request.headers.get('Connection'),
          secVersion: request.headers.get('Sec-WebSocket-Version'),
          contentLength: request.headers.get('Content-Length'),
          contentType: request.headers.get('Content-Type'),
        });
      }
      return this.forwardWebSocket(request);
    }

    if (!this.tunnel) {
      return new Response('Headscale not connected', { 
        status: 503,
        headers: { 'Retry-After': '30' }
      });
    }
    
    // Serialize request
    const requestId = crypto.randomUUID();
    const contentType = request.headers.get('Content-Type') || '';
    const isText =
      contentType.startsWith('text/') ||
      contentType.includes('json') ||
      contentType.includes('xml') ||
      contentType.includes('x-www-form-urlencoded');

    let body: string | undefined;
    let isBinary = false;
    if (request.body) {
      if (isText) {
        body = await request.text();
      } else {
        const buf = new Uint8Array(await request.arrayBuffer());
        body = toBase64(buf);
        isBinary = true;
        if (request.url.includes('/ts2021')) {
          const preview = Array.from(buf.subarray(0, 16)).map((b) => b.toString(16).padStart(2, '0')).join('');
          console.log('TS2021 HTTP body (binary)', { length: buf.length, previewHex: preview });
        }
      }
    }
    if (request.url.includes('/ts2021')) {
      console.log('TS2021 HTTP request', {
        host: request.headers.get('Host'),
        upgrade: upgradeHeader,
        wsKey: wsKey ? 'present' : 'missing',
        connection: request.headers.get('Connection'),
        contentLength: request.headers.get('Content-Length'),
        contentType: request.headers.get('Content-Type'),
        isBinary,
        bodyLength: body ? body.length : 0,
      });
    }

    const tunnelRequest: TunnelRequest = {
      id: requestId,
      method: request.method,
      url: request.url,
      headers: Object.fromEntries(request.headers),
      body,
      isBinary,
    };
    
    // Send through tunnel and wait for response
    return new Promise((resolve, reject) => {
      // Timeout after 30 seconds
      const timeout = setTimeout(() => {
        this.pendingRequests.delete(requestId);
        reject(new Error('Request timeout'));
      }, 30000);
      
      this.pendingRequests.set(requestId, { resolve, reject, timeout });
      
      const message: TunnelMessage = {
        type: 'request',
        data: tunnelRequest,
      };
      
      try {
        this.tunnel!.send(JSON.stringify(message));
      } catch (error) {
        clearTimeout(timeout);
        this.pendingRequests.delete(requestId);
        reject(error);
      }
    });
  }

  /**
   * Forward WebSocket upgrade through tunnel to Headscale
   */
  private async forwardWebSocket(request: Request): Promise<Response> {
    if (!this.tunnel) {
      return new Response('Headscale not connected', {
        status: 503,
        headers: { 'Retry-After': '30' }
      });
    }

    const upgradeHeader = request.headers.get('Upgrade');
    if (upgradeHeader !== 'websocket') {
      return new Response('Expected WebSocket', { status: 426 });
    }

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    server.accept();

    const wsId = crypto.randomUUID();
    this.wsConnections.set(wsId, server);

    // Send ws_open to sidecar
    const openMsg: TunnelMessage = {
      type: 'ws_open',
      data: {
        id: wsId,
        url: request.url,
        headers: Object.fromEntries(request.headers),
      } as WsOpen,
    };
    this.tunnel.send(JSON.stringify(openMsg));

    // Relay client -> sidecar
    server.addEventListener('message', async (event) => {
      try {
        if (!this.tunnel) return;
        const data = event.data;
        let msg: WsData;
        if (typeof data === 'string') {
          msg = { id: wsId, data, isBinary: false };
        } else {
          const buf = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(await data.arrayBuffer());
          const b64 = toBase64(buf);
          msg = { id: wsId, data: b64, isBinary: true };
        }
        this.tunnel.send(JSON.stringify({ type: 'ws_data', data: msg }));
      } catch (error) {
        console.error('Error relaying ws message:', error);
      }
    });

    server.addEventListener('close', (event) => {
      this.wsConnections.delete(wsId);
      if (this.tunnel) {
        const closeMsg: TunnelMessage = {
          type: 'ws_close',
          data: {
            id: wsId,
            code: event.code,
            reason: event.reason,
          } as WsClose,
        };
        this.tunnel.send(JSON.stringify(closeMsg));
      }
    });

    return new Response(null, {
      status: 101,
      webSocket: client,
    });
  }
  
  /**
   * Handle response from Headscale through tunnel
   */
  private handleTunnelMessage(data: string | ArrayBuffer) {
    try {
      const message: TunnelMessage = JSON.parse(data.toString());
      
      if (message.type === 'response') {
        const response = message.data as TunnelResponse;
        const pending = this.pendingRequests.get(response.id);
        
        if (pending) {
          clearTimeout(pending.timeout);
          this.pendingRequests.delete(response.id);
          
          // Reconstruct HTTP response
          let body: BodyInit | null = response.body ?? null;
          if (response.isBinary && response.body) {
            const binary = fromBase64(response.body);
            body = binary;
          }
          const httpResponse = new Response(body, {
            status: response.status,
            headers: response.headers,
          });
          
          pending.resolve(httpResponse);
        }
      } else if (message.type === 'ws_data') {
        const wsData = message.data as WsData;
        const ws = this.wsConnections.get(wsData.id);
        if (!ws) return;
        if (wsData.isBinary) {
          const binary = fromBase64(wsData.data);
          ws.send(binary);
        } else {
          ws.send(wsData.data);
        }
      } else if (message.type === 'ws_close') {
        const wsClose = message.data as WsClose;
        const ws = this.wsConnections.get(wsClose.id);
        if (ws) {
          ws.close(wsClose.code, wsClose.reason);
          this.wsConnections.delete(wsClose.id);
        }
      } else if (message.type === 'ping') {
        // Respond to keepalive
        this.tunnel?.send(JSON.stringify({ type: 'pong' }));
      }
    } catch (error) {
      console.error('Error handling tunnel message:', error);
    }
  }
}

function extractFingerprint(hostname: string): string | null {
  const match = hostname.match(/^([a-f0-9]{32})\.headfwd\.net$/i);
  return match ? match[1].toLowerCase() : null;
}

function toBase64(bytes: Uint8Array): string {
  const chunkSize = 0x8000;
  let binary = '';
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode(...chunk);
  }
  return btoa(binary);
}

function fromBase64(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
