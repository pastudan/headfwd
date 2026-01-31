# Cloudflare Durable Objects Implementation Plan

## Overview

Build the HeadFwd key exchange proxy using **Cloudflare Workers + Durable Objects** for serverless, global edge deployment.

**Architecture:** Workers handle routing → Durable Objects maintain persistent connections → Forward to user's Headscale  
**Language:** TypeScript (Workers standard)  
**Cost:** ~$5-20/month for 100k users  
**Deployment:** Global edge network (300+ locations)

---

## Why Durable Objects?

Traditional Workers are **stateless** (10ms CPU limit, no persistent connections).  
**Durable Objects** provide:

- ✅ Stateful compute (can hold WebSocket connections)
- ✅ Strong consistency (single-instance coordination)
- ✅ Persistent storage (key-value)
- ✅ Long-running connections (perfect for proxying)

---

## Architecture Deep Dive

```
┌──────────────────────────────────────────────────────────┐
│  Client Request: https://abc123...xyz.headfwd.net/key       │
└───────────────────────┬──────────────────────────────────┘
                        │
                        ▼
        ┌───────────────────────────────┐
        │  Cloudflare Worker (Edge)      │
        │  - Extract fingerprint         │
        │  - Route to Durable Object     │
        │  - Handle static requests      │
        └───────────┬───────────────────┘
                    │
                    ▼
        ┌───────────────────────────────┐
        │  Durable Object (Stateful)     │
        │  - Maintain connection pool    │
        │  - Proxy to Headscale          │
        │  - Cache routing info          │
        └───────────┬───────────────────┘
                    │
                    ▼
        ┌───────────────────────────────┐
        │  User's Headscale Instance     │
        │  https://headscale.example.com │
        └────────────────────────────────┘
```

### Key Components

1. **Worker (Stateless Edge Handler)**

   - Extracts fingerprint from subdomain
   - Routes to appropriate Durable Object
   - Handles health checks and registration API

2. **Durable Object (Stateful Proxy)**

   - One DO per active fingerprint
   - Maintains HTTP connection pool to target Headscale
   - Stores routing configuration in DO storage
   - Handles long-lived proxy connections

3. **KV Storage (Registry)**

   - Maps fingerprint → target URL
   - Globally replicated (low latency reads)
   - Fallback if DO doesn't have routing info

4. **R2 Storage (Optional - Logs/Analytics)**
   - Store connection logs
   - Analytics data
   - Cheaper than DO storage for large data

---

## Data Model

### KV Storage (Global Registry)

```typescript
interface EndpointRecord {
  fingerprint: string;
  targetUrl: string;
  createdAt: number;
  lastSeen: number;
  active: boolean;
  metadata?: {
    version?: string;
    region?: string;
  };
}

// KV Key: `endpoint:${fingerprint}`
// KV Value: JSON.stringify(EndpointRecord)
```

### Durable Object Storage (Per-Fingerprint State)

```typescript
interface ProxyState {
  fingerprint: string;
  targetUrl: string;
  connectionCount: number;
  lastActivity: number;
  cacheExpiry: number;
}

// DO Storage Key: `state`
// DO Storage Value: JSON.stringify(ProxyState)
```

### DO Memory (Connection Pool)

```typescript
class ProxyDurableObject {
  private connectionPool: Map<string, ConnectionInfo>;
  private targetUrl: string;
  private stats: ProxyStats;
}
```

---

## Phase 1: Basic Worker + KV Registry (Days 1-3)

**Goal:** Handle subdomain routing with static configuration

### 1.1 - Project Setup (Day 1)

```bash
# Create Wrangler project
npm create cloudflare@latest keyexchange-proxy
cd keyexchange-proxy

# Choose:
# - "Hello World" Worker template
# - TypeScript
# - Git repository: yes
# - Deploy: no (we'll do it manually)

# Install dependencies
npm install
```

**File Structure:**

```
keyexchange-proxy/
├── src/
│   ├── index.ts           # Main Worker entry
│   ├── registry.ts        # KV registry operations
│   ├── types.ts           # TypeScript types
│   └── utils.ts           # Helper functions
├── wrangler.toml          # Cloudflare config
├── package.json
└── tsconfig.json
```

**wrangler.toml:**

```toml
name = "keyexchange-proxy"
main = "src/index.ts"
compatibility_date = "2024-01-01"

[env.production]
routes = [
  { pattern = "*.headfwd.net/*", zone_name = "headfwd.net" }
]

[[kv_namespaces]]
binding = "REGISTRY"
id = "your-kv-namespace-id"
```

### Tasks:

- [ ] Initialize Wrangler project
- [ ] Set up TypeScript configuration
- [ ] Create KV namespace: `wrangler kv:namespace create "REGISTRY"`
- [ ] Configure wrangler.toml with KV binding
- **Test:** `wrangler dev` runs locally

### 1.2 - Basic Worker Routing (Day 2)

**src/index.ts:**

```typescript
export interface Env {
  REGISTRY: KVNamespace;
  PROXY: DurableObjectNamespace;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const hostname = url.hostname;

    // Extract fingerprint from subdomain
    const fingerprint = extractFingerprint(hostname);

    if (!fingerprint) {
      return new Response("Invalid subdomain", { status: 400 });
    }

    // Lookup target URL from KV
    const endpointKey = `endpoint:${fingerprint}`;
    const endpointData = await env.REGISTRY.get(endpointKey, "json");

    if (!endpointData) {
      return new Response("Endpoint not found", { status: 404 });
    }

    const endpoint = endpointData as EndpointRecord;

    // Simple proxy (will move to DO later)
    return proxyRequest(request, endpoint.targetUrl);
  },
};

function extractFingerprint(hostname: string): string | null {
  // Match: <fingerprint>.headfwd.net
  const match = hostname.match(/^([a-f0-9]{64})\.k8g8\.com$/i);
  return match ? match[1] : null;
}

async function proxyRequest(
  request: Request,
  targetUrl: string
): Promise<Response> {
  const url = new URL(request.url);
  url.hostname = new URL(targetUrl).hostname;

  const proxyRequest = new Request(url.toString(), {
    method: request.method,
    headers: request.headers,
    body: request.body,
  });

  return fetch(proxyRequest);
}
```

### Tasks:

- [ ] Implement fingerprint extraction
- [ ] Add KV lookup
- [ ] Basic fetch-based proxying
- [ ] Add error handling
- **Test:** Register test endpoint in KV, proxy request through Worker

### 1.3 - Registration API (Day 3)

**src/index.ts (add route):**

```typescript
export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    // Registration API endpoint
    if (url.pathname === "/api/register" && request.method === "POST") {
      return handleRegister(request, env);
    }

    // ... rest of proxy logic
  },
};

async function handleRegister(request: Request, env: Env): Promise<Response> {
  // Verify auth token
  const authHeader = request.headers.get("Authorization");
  if (!verifyAuthToken(authHeader, env)) {
    return new Response("Unauthorized", { status: 401 });
  }

  const body = await request.json();
  const { pubkey, targetUrl } = body;

  // Validate inputs
  if (!pubkey || !targetUrl) {
    return new Response("Missing pubkey or targetUrl", { status: 400 });
  }

  // Compute SHA256 fingerprint
  const fingerprint = await computeFingerprint(pubkey);

  // Validate target URL is reachable
  const isReachable = await checkTargetHealth(targetUrl);
  if (!isReachable) {
    return new Response("Target URL unreachable", { status: 400 });
  }

  // Store in KV
  const endpoint: EndpointRecord = {
    fingerprint,
    targetUrl,
    createdAt: Date.now(),
    lastSeen: Date.now(),
    active: true,
  };

  await env.REGISTRY.put(
    `endpoint:${fingerprint}`,
    JSON.stringify(endpoint),
    { expirationTtl: 60 * 60 * 24 * 365 } // 1 year
  );

  return new Response(
    JSON.stringify({
      success: true,
      fingerprint,
      subdomain: `${fingerprint}.headfwd.net`,
    }),
    {
      headers: { "Content-Type": "application/json" },
    }
  );
}

async function computeFingerprint(pubkey: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(pubkey);
  const hashBuffer = await crypto.subtle.digest("SHA-256", data);
  const hashArray = Array.from(new Uint8Array(hashBuffer));
  return hashArray.map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function checkTargetHealth(targetUrl: string): Promise<boolean> {
  try {
    const response = await fetch(`${targetUrl}/health`, {
      method: "GET",
      signal: AbortSignal.timeout(5000), // 5s timeout
    });
    return response.ok;
  } catch {
    return false;
  }
}
```

### Tasks:

- [ ] Implement `/api/register` endpoint
- [ ] Add SHA256 fingerprint computation
- [ ] Validate target URL health
- [ ] Store in KV with TTL
- [ ] Add authentication (bearer token)
- **Test:** Register endpoint via API, verify KV entry

---

## Phase 2: Durable Objects for Stateful Proxying (Days 4-7)

**Goal:** Move proxy logic to Durable Objects for connection pooling and state

### 2.1 - Durable Object Setup (Day 4)

**wrangler.toml (add DO binding):**

```toml
[[durable_objects.bindings]]
name = "PROXY"
class_name = "ProxyDurableObject"
script_name = "keyexchange-proxy"

[[migrations]]
tag = "v1"
new_classes = ["ProxyDurableObject"]
```

**src/proxy-do.ts:**

```typescript
export class ProxyDurableObject {
  private state: DurableObjectState;
  private env: Env;
  private targetUrl: string | null = null;
  private connectionCount: number = 0;

  constructor(state: DurableObjectState, env: Env) {
    this.state = state;
    this.env = env;

    // Load state from storage
    this.state.blockConcurrencyWhile(async () => {
      const storedState = await this.state.storage.get<ProxyState>("state");
      if (storedState) {
        this.targetUrl = storedState.targetUrl;
        this.connectionCount = storedState.connectionCount || 0;
      }
    });
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    // Initialize endpoint if not loaded
    if (!this.targetUrl) {
      const fingerprint = url.searchParams.get("fingerprint");
      if (!fingerprint) {
        return new Response("Missing fingerprint", { status: 400 });
      }

      await this.loadEndpoint(fingerprint);
    }

    if (!this.targetUrl) {
      return new Response("Endpoint not configured", { status: 404 });
    }

    // Proxy the request
    return this.proxyRequest(request);
  }

  private async loadEndpoint(fingerprint: string) {
    // Check DO storage first (cache)
    let state = await this.state.storage.get<ProxyState>("state");

    if (!state || Date.now() > state.cacheExpiry) {
      // Fetch from KV
      const endpointData = await this.env.REGISTRY.get(
        `endpoint:${fingerprint}`,
        "json"
      );

      if (!endpointData) {
        return;
      }

      const endpoint = endpointData as EndpointRecord;

      // Cache in DO storage
      state = {
        fingerprint,
        targetUrl: endpoint.targetUrl,
        connectionCount: 0,
        lastActivity: Date.now(),
        cacheExpiry: Date.now() + 5 * 60 * 1000, // 5 min cache
      };

      await this.state.storage.put("state", state);
    }

    this.targetUrl = state.targetUrl;
  }

  private async proxyRequest(request: Request): Promise<Response> {
    if (!this.targetUrl) {
      return new Response("No target URL", { status: 500 });
    }

    // Increment connection count
    this.connectionCount++;

    // Build target URL
    const url = new URL(request.url);
    const targetHost = new URL(this.targetUrl).host;
    url.host = targetHost;

    // Create proxied request
    const proxyHeaders = new Headers(request.headers);
    proxyHeaders.set(
      "X-Forwarded-For",
      request.headers.get("CF-Connecting-IP") || ""
    );
    proxyHeaders.set("X-Forwarded-Proto", "https");

    try {
      const response = await fetch(url.toString(), {
        method: request.method,
        headers: proxyHeaders,
        body: request.body,
        redirect: "manual",
      });

      // Update last activity
      await this.updateActivity();

      return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
      });
    } catch (error) {
      console.error("Proxy error:", error);
      return new Response("Bad Gateway", { status: 502 });
    } finally {
      this.connectionCount--;
    }
  }

  private async updateActivity() {
    const state = await this.state.storage.get<ProxyState>("state");
    if (state) {
      state.lastActivity = Date.now();
      state.connectionCount = this.connectionCount;
      await this.state.storage.put("state", state);
    }
  }
}
```

### Tasks:

- [ ] Create Durable Object class
- [ ] Implement state loading (KV → DO storage)
- [ ] Add proxy logic with connection tracking
- [ ] Cache endpoint config in DO storage
- [ ] Update activity timestamps
- **Test:** Route request through DO, verify proxying

### 2.2 - Worker → DO Integration (Day 5)

**src/index.ts (update):**

```typescript
export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const hostname = url.hostname;

    // Extract fingerprint
    const fingerprint = extractFingerprint(hostname);
    if (!fingerprint) {
      return new Response("Invalid subdomain", { status: 400 });
    }

    // Get Durable Object instance
    const doId = env.PROXY.idFromName(fingerprint);
    const doStub = env.PROXY.get(doId);

    // Forward to DO with fingerprint
    const doUrl = new URL(request.url);
    doUrl.searchParams.set("fingerprint", fingerprint);

    const doRequest = new Request(doUrl.toString(), {
      method: request.method,
      headers: request.headers,
      body: request.body,
    });

    return doStub.fetch(doRequest);
  },
};

// Export DO class
export { ProxyDurableObject } from "./proxy-do";
```

### Tasks:

- [ ] Route Worker requests to DO
- [ ] Use deterministic DO ID (from fingerprint)
- [ ] Pass fingerprint to DO
- [ ] Export DO class in Worker
- **Test:** End-to-end: Register → Proxy via Worker → DO → Target

### 2.3 - Connection Pooling & Reuse (Day 6)

**src/proxy-do.ts (enhance):**

```typescript
interface ConnectionPoolEntry {
  lastUsed: number;
  inUse: boolean;
}

export class ProxyDurableObject {
  private connectionPool: Map<string, ConnectionPoolEntry> = new Map();
  private maxPoolSize = 100;

  // ... existing code ...

  private async proxyRequest(request: Request): Promise<Response> {
    // Get or create connection
    const connectionId = this.getConnectionId();

    // Track in pool
    this.connectionPool.set(connectionId, {
      lastUsed: Date.now(),
      inUse: true,
    });

    try {
      const response = await fetch(/* ... */);

      // Mark connection as available
      const poolEntry = this.connectionPool.get(connectionId);
      if (poolEntry) {
        poolEntry.inUse = false;
        poolEntry.lastUsed = Date.now();
      }

      return response;
    } finally {
      // Cleanup old connections
      this.cleanupPool();
    }
  }

  private getConnectionId(): string {
    return `conn-${Date.now()}-${Math.random().toString(36)}`;
  }

  private cleanupPool() {
    const now = Date.now();
    const maxAge = 90 * 1000; // 90s

    for (const [id, entry] of this.connectionPool.entries()) {
      if (!entry.inUse && now - entry.lastUsed > maxAge) {
        this.connectionPool.delete(id);
      }
    }

    // Enforce max pool size
    if (this.connectionPool.size > this.maxPoolSize) {
      const sorted = Array.from(this.connectionPool.entries()).sort(
        (a, b) => a[1].lastUsed - b[1].lastUsed
      );

      for (let i = 0; i < sorted.length - this.maxPoolSize; i++) {
        this.connectionPool.delete(sorted[i][0]);
      }
    }
  }
}
```

### Tasks:

- [ ] Implement connection pool in DO
- [ ] Track connection usage
- [ ] Cleanup stale connections
- [ ] Enforce max pool size
- **Test:** High concurrency (100 requests), verify pool management

### 2.4 - WebSocket Support (Day 7)

**src/proxy-do.ts (add WebSocket):**

```typescript
export class ProxyDurableObject {
  async fetch(request: Request): Promise<Response> {
    // Check for WebSocket upgrade
    const upgradeHeader = request.headers.get("Upgrade");
    if (upgradeHeader === "websocket") {
      return this.handleWebSocket(request);
    }

    // ... regular HTTP proxy
  }

  private async handleWebSocket(request: Request): Promise<Response> {
    if (!this.targetUrl) {
      return new Response("Not configured", { status: 404 });
    }

    // Create WebSocket pair
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    // Accept client connection
    server.accept();

    // Connect to target
    const targetUrl = new URL(request.url);
    targetUrl.protocol = "wss:";
    targetUrl.host = new URL(this.targetUrl).host;

    const targetWs = new WebSocket(targetUrl.toString());

    // Pipe messages bidirectionally
    server.addEventListener("message", (event) => {
      if (targetWs.readyState === WebSocket.OPEN) {
        targetWs.send(event.data);
      }
    });

    targetWs.addEventListener("message", (event) => {
      if (server.readyState === WebSocket.OPEN) {
        server.send(event.data);
      }
    });

    // Handle close
    server.addEventListener("close", () => targetWs.close());
    targetWs.addEventListener("close", () => server.close());

    return new Response(null, {
      status: 101,
      webSocket: client,
    });
  }
}
```

### Tasks:

- [ ] Detect WebSocket upgrade requests
- [ ] Create WebSocket pair
- [ ] Connect to target WebSocket
- [ ] Pipe messages bidirectionally
- [ ] Handle connection close
- **Test:** WebSocket connection through proxy

---

## Phase 3: Security & Rate Limiting (Days 8-10)

### 3.1 - Authentication (Day 8)

**src/auth.ts:**

```typescript
export async function verifyRegistrationToken(
  authHeader: string | null,
  env: Env
): Promise<boolean> {
  if (!authHeader || !authHeader.startsWith("Bearer ")) {
    return false;
  }

  const token = authHeader.substring(7);

  // Check against stored tokens in KV
  const validToken = await env.REGISTRY.get(`token:${token}`);
  return validToken !== null;
}

export async function createRegistrationToken(env: Env): Promise<string> {
  // Generate secure random token
  const buffer = new Uint8Array(32);
  crypto.getRandomValues(buffer);
  const token = Array.from(buffer)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

  // Store in KV
  await env.REGISTRY.put(`token:${token}`, "valid", {
    expirationTtl: 60 * 60 * 24 * 365, // 1 year
  });

  return token;
}
```

### Tasks:

- [ ] Implement token generation
- [ ] Store tokens in KV
- [ ] Verify tokens on registration
- [ ] Add token management API
- **Test:** Register with valid/invalid tokens

### 3.2 - Rate Limiting (Day 9)

**src/rate-limit.ts:**

```typescript
interface RateLimitInfo {
  count: number;
  resetAt: number;
}

export class RateLimiter {
  constructor(private env: Env) {}

  async checkRateLimit(
    key: string,
    limit: number,
    windowMs: number
  ): Promise<{ allowed: boolean; remaining: number }> {
    const rateLimitKey = `ratelimit:${key}`;

    // Get current count
    const data = await this.env.REGISTRY.get<RateLimitInfo>(
      rateLimitKey,
      "json"
    );

    const now = Date.now();

    if (!data || now > data.resetAt) {
      // New window
      await this.env.REGISTRY.put(
        rateLimitKey,
        JSON.stringify({
          count: 1,
          resetAt: now + windowMs,
        }),
        {
          expirationTtl: Math.ceil(windowMs / 1000),
        }
      );

      return { allowed: true, remaining: limit - 1 };
    }

    if (data.count >= limit) {
      return { allowed: false, remaining: 0 };
    }

    // Increment count
    data.count++;
    await this.env.REGISTRY.put(rateLimitKey, JSON.stringify(data), {
      expirationTtl: Math.ceil((data.resetAt - now) / 1000),
    });

    return { allowed: true, remaining: limit - data.count };
  }
}

// Usage in Worker
export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    // Rate limit by IP
    const clientIp = request.headers.get("CF-Connecting-IP") || "unknown";
    const limiter = new RateLimiter(env);

    const { allowed, remaining } = await limiter.checkRateLimit(
      `registration:${clientIp}`,
      5, // 5 requests
      60 * 60 * 1000 // per hour
    );

    if (!allowed) {
      return new Response("Rate limit exceeded", {
        status: 429,
        headers: {
          "Retry-After": "3600",
        },
      });
    }

    // ... handle request
  },
};
```

### Tasks:

- [ ] Implement rate limiter class
- [ ] Use KV for rate limit tracking
- [ ] Apply to registration endpoint
- [ ] Add retry-after headers
- [ ] Per-fingerprint rate limiting for proxying
- **Test:** Exceed rate limit, verify 429 response

### 3.3 - Security Headers & DDoS Protection (Day 10)

**src/security.ts:**

```typescript
export function addSecurityHeaders(response: Response): Response {
  const headers = new Headers(response.headers);

  headers.set(
    "Strict-Transport-Security",
    "max-age=31536000; includeSubDomains"
  );
  headers.set("X-Content-Type-Options", "nosniff");
  headers.set("X-Frame-Options", "DENY");
  headers.set("X-XSS-Protection", "1; mode=block");
  headers.set("Referrer-Policy", "strict-origin-when-cross-origin");

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}

export async function checkFirewall(request: Request): Promise<boolean> {
  // Use Cloudflare's threat score
  const threatScore = request.cf?.threatScore as number | undefined;

  if (threatScore && threatScore > 50) {
    return false; // Block high-threat requests
  }

  // Additional checks
  const userAgent = request.headers.get("User-Agent");
  if (!userAgent || userAgent.length < 10) {
    return false; // Block suspicious UAs
  }

  return true;
}
```

### Tasks:

- [ ] Add security headers to all responses
- [ ] Implement firewall checks (Cloudflare threat score)
- [ ] Block suspicious requests
- [ ] Add DDoS protection rules
- **Test:** Security headers present, threat detection works

---

## Phase 4: Monitoring & Observability (Days 11-12)

### 4.1 - Analytics & Logging (Day 11)

**src/analytics.ts:**

```typescript
interface AnalyticsEvent {
  timestamp: number;
  fingerprint: string;
  event: "request" | "error" | "register";
  statusCode?: number;
  duration?: number;
  clientIp?: string;
}

export class Analytics {
  constructor(private env: Env) {}

  async logEvent(event: AnalyticsEvent) {
    // Use Workers Analytics Engine
    // (requires separate binding in wrangler.toml)

    // Or write to R2 for batch processing
    const key = `analytics/${event.timestamp}-${event.fingerprint}.json`;
    // await this.env.ANALYTICS_BUCKET.put(key, JSON.stringify(event));

    // Or use KV for aggregated metrics
    const metricsKey = `metrics:${event.fingerprint}:${getDateKey()}`;
    const current = (await this.env.REGISTRY.get<number>(metricsKey)) || 0;
    await this.env.REGISTRY.put(metricsKey, (current + 1).toString(), {
      expirationTtl: 60 * 60 * 24 * 7, // 7 days
    });
  }
}

function getDateKey(): string {
  const now = new Date();
  return `${now.getUTCFullYear()}-${now.getUTCMonth() + 1}-${now.getUTCDate()}`;
}
```

### Tasks:

- [ ] Implement analytics logging
- [ ] Track requests per fingerprint
- [ ] Log errors and latency
- [ ] Store aggregated metrics in KV
- **Test:** Generate traffic, verify metrics

### 4.2 - Metrics API (Day 12)

**src/index.ts (add endpoint):**

```typescript
async function handleMetrics(env: Env): Promise<Response> {
  const metrics = {
    totalEndpoints: 0,
    activeConnections: 0,
    requestsToday: 0,
    topFingerprints: [] as any[],
  };

  // Count endpoints
  const endpointList = await env.REGISTRY.list({ prefix: "endpoint:" });
  metrics.totalEndpoints = endpointList.keys.length;

  // Get today's metrics
  const dateKey = getDateKey();
  const requestsKey = `metrics:total:${dateKey}`;
  const requestsCount = await env.REGISTRY.get<number>(requestsKey);
  metrics.requestsToday = requestsCount || 0;

  return new Response(JSON.stringify(metrics, null, 2), {
    headers: { "Content-Type": "application/json" },
  });
}
```

### Tasks:

- [ ] Create `/api/metrics` endpoint
- [ ] Aggregate stats from KV
- [ ] Add authentication (admin only)
- [ ] Return JSON metrics
- **Test:** Fetch metrics, verify accuracy

---

## Phase 5: Testing & Deployment (Days 13-14)

### 5.1 - Integration Tests (Day 13)

**tests/integration.test.ts:**

```typescript
import { unstable_dev } from "wrangler";

describe("Proxy Integration Tests", () => {
  let worker: any;

  beforeAll(async () => {
    worker = await unstable_dev("src/index.ts", {
      experimental: { disableExperimentalWarning: true },
    });
  });

  afterAll(async () => {
    await worker.stop();
  });

  test("Register endpoint", async () => {
    const response = await worker.fetch("https://headfwd.net/api/register", {
      method: "POST",
      headers: {
        Authorization: "Bearer test-token",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        pubkey: "test-public-key",
        targetUrl: "https://headscale.example.com",
      }),
    });

    expect(response.status).toBe(200);
    const data = await response.json();
    expect(data.fingerprint).toBeDefined();
    expect(data.subdomain).toContain(".headfwd.net");
  });

  test("Proxy request to registered endpoint", async () => {
    const fingerprint = "abc123..."; // From registration

    const response = await worker.fetch(
      `https://${fingerprint}.headfwd.net/health`
    );

    expect(response.ok).toBe(true);
  });

  test("Rate limiting works", async () => {
    // Make 6 requests (limit is 5/hour)
    for (let i = 0; i < 6; i++) {
      const response = await worker.fetch("https://headfwd.net/api/register", {
        method: "POST",
        /* ... */
      });

      if (i < 5) {
        expect(response.status).not.toBe(429);
      } else {
        expect(response.status).toBe(429);
      }
    }
  });
});
```

### Tasks:

- [ ] Set up Vitest or Jest
- [ ] Write integration tests for registration
- [ ] Test proxy functionality
- [ ] Test rate limiting
- [ ] Test error cases
- **Test:** All tests pass

### 5.2 - Production Deployment (Day 14)

**Deploy to Cloudflare:**

```bash
# Create production KV namespace
wrangler kv:namespace create "REGISTRY" --preview false

# Update wrangler.toml with production KV ID

# Deploy
wrangler deploy

# Verify deployment
curl https://headfwd.net/health
```

**DNS Setup:**

```bash
# Add wildcard DNS in Cloudflare dashboard:
# Type: CNAME
# Name: *
# Content: headfwd.net
# Proxy status: Proxied (orange cloud)

# Add apex record:
# Type: A
# Name: @
# Content: <worker-ip> (or CNAME to workers.dev)
# Proxy status: Proxied
```

### Tasks:

- [ ] Deploy Worker to production
- [ ] Configure DNS (wildcard + apex)
- [ ] Generate production auth tokens
- [ ] Set up custom domain
- [ ] Verify end-to-end flow
- **Test:** Register real endpoint, proxy request from phone

---

## Technology Stack

| Component        | Technology               | Purpose                   |
| ---------------- | ------------------------ | ------------------------- |
| Edge Compute     | Cloudflare Workers       | Request routing           |
| Stateful Compute | Durable Objects          | Connection pooling, proxy |
| Registry Storage | Cloudflare KV            | Fingerprint → URL mapping |
| Analytics        | Workers Analytics Engine | Request tracking          |
| Language         | TypeScript               | Workers standard          |
| Testing          | Vitest + Wrangler        | Integration tests         |
| Deployment       | Wrangler CLI             | Deploy to Cloudflare      |

---

## Cost Breakdown (Detailed)

### Free Tier

- **Workers:** 100k requests/day
- **KV:** 100k reads/day, 1k writes/day, 1GB storage
- **Durable Objects:** 1M requests/month

### Paid Tier ($5/month Workers Paid)

- **Workers:** $5/month + $0.50/million requests (after 10M)
- **KV:** $0.50/million reads (after 100k/day), $5/million writes
- **Durable Objects:**
  - $0.15/million requests
  - $0.20/million GB-seconds of compute
  - $0.20/GB storage per month

### Example Costs for 100k Users

**Assumptions:**

- 10 requests/user/month = 1M requests
- 100 active DOs at any time
- 10MB KV storage

**Monthly Cost:**

- Workers: $5 (base)
- KV: $0 (within free tier)
- DO Requests: $0.15
- DO Compute: ~$1
- **Total: ~$6-7/month**

**At scale (1M users, 10M requests/month):**

- Workers: $5
- KV: $1
- DO Requests: $1.50
- DO Compute: ~$5
- **Total: ~$12-15/month**

---

## Deployment Checklist

- [ ] Create Cloudflare account
- [ ] Install Wrangler CLI: `npm install -g wrangler`
- [ ] Authenticate: `wrangler login`
- [ ] Create KV namespace
- [ ] Configure wrangler.toml
- [ ] Deploy Worker: `wrangler deploy`
- [ ] Add custom domain in Cloudflare dashboard
- [ ] Configure wildcard DNS
- [ ] Generate production auth tokens
- [ ] Test registration flow
- [ ] Test proxy flow
- [ ] Set up monitoring alerts

---

## Advantages Over Traditional VPS

✅ **Global edge network** (300+ locations)  
✅ **Auto-scaling** (no capacity planning)  
✅ **Built-in DDoS protection**  
✅ **Zero DevOps** (no servers to manage)  
✅ **Pay-per-use** (no idle costs)  
✅ **Integrated services** (KV, R2, Analytics)  
✅ **Fast cold starts** (<1ms)

---

## Disadvantages

❌ **Vendor lock-in** (Cloudflare-specific APIs)  
❌ **10ms CPU limit** per Worker invocation (DO has longer limits)  
❌ **Learning curve** for DO programming model  
❌ **Debugging** can be harder than traditional servers  
❌ **Storage limits** (KV is key-value only, no SQL)

---

## Next Steps

1. **Initialize Wrangler project** (Phase 1.1)
2. **Build basic Worker** with KV registry (Phase 1.2)
3. **Add Durable Objects** for stateful proxy (Phase 2)
4. **Deploy to Cloudflare** (Phase 5.2)
5. **Test with real Headscale instance**

Ready to start? Run:

```bash
npm create cloudflare@latest keyexchange-proxy
cd keyexchange-proxy
wrangler dev
```

🚀
