# Key Exchange Traffic Forwarding - Development Plan

## Overview

Build a minimal proxy service for HeadFwd's key exchange and connection bootstrapping.

**Purpose:** Route traffic from `<pubkey-fingerprint>.headfwd.net` to user's self-hosted Headscale instance  
**Core principle:** Zero knowledge of payloads, keys, or credentials—pure routing only  
**Language:** **Go** (excellent networking, simple deployment, proven for proxy services)

---

## Why Go?

- **Battle-tested reverse proxy patterns** (see Caddy, Traefik)
- Fast compilation, single binary deployment
- Excellent standard library for HTTP/TCP proxying
- Built-in concurrency (goroutines) for handling many connections
- Lower complexity than Rust for this use case
- Easy to containerize and deploy

**Alternative:** Rust if you need absolute maximum performance, but Go is the pragmatic choice here.

---

## Architecture

```
┌─────────────┐
│ iOS Client  │
└──────┬──────┘
       │ HTTPS
       ▼
┌──────────────────────────────────┐
│  keyexchange.headfwd.net            │
│  (Go Proxy Service)              │
│                                  │
│  • Wildcard DNS: *.headfwd.net      │
│  • Route by subdomain            │
│  • Registry: pubkey → endpoint   │
│  • Connection cache              │
└──────┬───────────────────────────┘
       │ HTTPS
       ▼
┌──────────────────────────┐
│ User's Headscale Instance│
│ (e.g., home.example.com) │
└──────────────────────────┘
```

---

## Core Data Model

### Registry Entry

```go
type HeadscaleEndpoint struct {
    PubkeyFingerprint string    // SHA256 of public key (subdomain)
    TargetURL         string    // User's Headscale HTTPS endpoint
    CreatedAt         time.Time
    LastSeen          time.Time
    Active            bool
}
```

### Connection Cache

```go
type ActiveConnection struct {
    PubkeyFingerprint string
    ClientIP          string
    ConnectedAt       time.Time
    LastActivity      time.Time
}
```

---

## Phase 1: Basic HTTP Proxy (Days 1-3)

**Goal:** Route subdomain traffic to configured backends

### 1.1 - Project Setup (Day 1)

- [ ] Initialize Go module: `go mod init github.com/yourorg/keyexchange`
- [ ] Set up project structure:
  ```
  keyexchange/
  ├── cmd/
  │   └── server/
  │       └── main.go
  ├── internal/
  │   ├── proxy/
  │   │   └── proxy.go
  │   ├── registry/
  │   │   └── registry.go
  │   └── config/
  │       └── config.go
  ├── go.mod
  └── README.md
  ```
- [ ] Add dependencies: `chi` (router), `sqlc` (DB), `pq` (Postgres driver)
- **Test:** `go build ./cmd/server` compiles successfully

### 1.2 - Basic Reverse Proxy (Day 2)

- [ ] Implement simple HTTP reverse proxy handler
- [ ] Extract subdomain from `Host` header
- [ ] Hardcoded routing for testing (e.g., `test123.headfwd.net` → `http://localhost:3000`)
- [ ] Add request/response logging
- **Test:** `curl https://test123.headfwd.net/health` proxies to backend

### 1.3 - Subdomain Extraction & Routing (Day 3)

- [ ] Parse `<fingerprint>.headfwd.net` from requests
- [ ] Validate fingerprint format (64 hex chars = SHA256)
- [ ] Return 404 for invalid/unknown fingerprints
- [ ] Add metrics (requests per fingerprint)
- **Test:** Valid fingerprint routes, invalid returns 404

---

## Phase 2: Registry Storage (Days 4-6)

**Goal:** Persistent mapping of pubkey fingerprints to Headscale endpoints

### 2.1 - Database Setup (Day 4)

- [ ] Choose storage: **PostgreSQL** (reliable, ACID, good for production)
- [ ] Alternative: **SQLite** for single-server deployments
- [ ] Create schema:

  ```sql
  CREATE TABLE headscale_endpoints (
      fingerprint TEXT PRIMARY KEY,
      target_url TEXT NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      active BOOLEAN NOT NULL DEFAULT true,
      metadata JSONB
  );

  CREATE INDEX idx_active_endpoints ON headscale_endpoints(active)
  WHERE active = true;
  ```

- [ ] Set up migrations (golang-migrate or goose)
- **Test:** Create table, insert test row

### 2.2 - Registry Service (Day 5)

- [ ] Implement `registry.Service` interface:
  ```go
  type Service interface {
      Register(fingerprint, targetURL string) error
      Lookup(fingerprint string) (*HeadscaleEndpoint, error)
      UpdateLastSeen(fingerprint string) error
      Deactivate(fingerprint string) error
  }
  ```
- [ ] Add connection pooling
- [ ] Implement CRUD operations
- **Test:** Unit tests for each method

### 2.3 - Registration API (Day 6)

- [ ] `POST /api/register` - register new endpoint
  ```json
  {
    "pubkey": "base64encodedkey...",
    "target_url": "https://headscale.example.com"
  }
  ```
- [ ] Compute SHA256 fingerprint from pubkey
- [ ] Validate target URL is reachable (health check)
- [ ] Return assigned subdomain: `<fingerprint>.headfwd.net`
- [ ] Add authentication (bearer token or mutual TLS)
- **Test:** Register endpoint, verify in DB

---

## Phase 3: Proxy Integration (Days 7-9)

**Goal:** Connect proxy to registry for dynamic routing

### 3.1 - Dynamic Routing (Day 7)

- [ ] Integrate registry lookup in proxy handler
- [ ] Cache lookups in memory (TTL: 5 minutes)
- [ ] Handle cache misses gracefully
- [ ] Update `last_seen` on each proxied request
- **Test:** Register endpoint → proxy request → verify routing

### 3.2 - Connection Pooling (Day 8)

- [ ] Implement HTTP client pool per backend
- [ ] Configure timeouts:
  - Connect: 5s
  - Request: 30s
  - Idle connection: 90s
- [ ] Limit concurrent connections per backend
- **Test:** High load test (100 concurrent requests)

### 3.3 - Error Handling (Day 9)

- [ ] Handle backend unreachable (503 Service Unavailable)
- [ ] Retry logic (1 retry with exponential backoff)
- [ ] Circuit breaker pattern (mark endpoint inactive after N failures)
- [ ] Proper HTTP status codes forwarding
- **Test:** Kill backend, verify error responses

---

## Phase 4: Connection Cache (Days 10-11)

**Goal:** Track active connections for debugging and rate limiting

### 4.1 - In-Memory Connection Tracking (Day 10)

- [ ] Create connection cache (sync.Map or Redis)
- [ ] Record on each proxy request:
  - Client IP
  - Fingerprint
  - Timestamp
- [ ] Expire entries after 10 minutes of inactivity
- [ ] Background goroutine for cleanup
- **Test:** Make requests, verify cache entries

### 4.2 - Metrics Endpoint (Day 11)

- [ ] `GET /api/metrics` - return stats:
  ```json
  {
    "active_connections": 42,
    "registered_endpoints": 100,
    "requests_last_hour": 1523,
    "by_fingerprint": { ... }
  }
  ```
- [ ] Optionally integrate Prometheus metrics
- **Test:** Verify counts match actual traffic

---

## Phase 5: Security & Auth (Days 12-14)

**Goal:** Prevent abuse and unauthorized registrations

### 5.1 - Registration Authentication (Day 12)

- [ ] Generate API keys for registration
- [ ] Store keys in DB with rate limits
- [ ] Require `Authorization: Bearer <key>` header
- [ ] Add key management endpoints (admin only)
- **Test:** Reject requests without valid key

### 5.2 - Rate Limiting (Day 13)

- [ ] Per-IP rate limit on registration (5/hour)
- [ ] Per-fingerprint rate limit on proxying (1000/min)
- [ ] Use token bucket algorithm
- [ ] Return 429 Too Many Requests
- **Test:** Exceed limits, verify rejection

### 5.3 - Security Headers & HTTPS (Day 14)

- [ ] Enforce HTTPS only
- [ ] Add security headers:
  - `Strict-Transport-Security`
  - `X-Content-Type-Options`
  - `X-Frame-Options`
- [ ] Configure TLS certificates (Let's Encrypt)
- [ ] Set up automatic cert renewal
- **Test:** SSL Labs scan, verify A+ rating

---

## Phase 6: DNS & Deployment (Days 15-17)

### 6.1 - DNS Configuration (Day 15)

- [ ] Set up wildcard DNS: `*.headfwd.net → <proxy-server-ip>`
- [ ] Configure apex domain: `headfwd.net` (landing page)
- [ ] Add health check subdomain: `health.headfwd.net`
- [ ] Verify DNS propagation
- **Test:** `dig abc123.headfwd.net` returns correct IP

### 6.2 - Docker Containerization (Day 16)

- [ ] Create `Dockerfile`:

  ```dockerfile
  FROM golang:1.21-alpine AS builder
  WORKDIR /build
  COPY . .
  RUN go build -o keyexchange ./cmd/server

  FROM alpine:latest
  RUN apk add --no-cache ca-certificates
  COPY --from=builder /build/keyexchange /usr/local/bin/
  EXPOSE 443
  CMD ["keyexchange"]
  ```

- [ ] Create `docker-compose.yml` with Postgres
- [ ] Add health check endpoint
- **Test:** `docker compose up` starts successfully

### 6.3 - Production Deployment (Day 17)

- [ ] Deploy to cloud (DigitalOcean/AWS/Fly.io)
- [ ] Set up load balancing (multiple instances)
- [ ] Configure environment variables
- [ ] Set up monitoring (logs, uptime)
- [ ] Create runbook for common issues
- **Test:** Full end-to-end flow in production

---

## Phase 7: Admin Tools & Monitoring (Days 18-19)

### 7.1 - Admin CLI (Day 18)

- [ ] Create CLI tool for management:
  ```bash
  keyexchange-cli register --pubkey <key> --target <url>
  keyexchange-cli list
  keyexchange-cli deactivate <fingerprint>
  keyexchange-cli stats
  ```
- [ ] Uses API with admin bearer token
- **Test:** Run all commands successfully

### 7.2 - Observability (Day 19)

- [ ] Structured logging (JSON format)
- [ ] Add request tracing IDs
- [ ] Integrate with logging service (e.g., Loki)
- [ ] Set up alerts:
  - High error rate (>5%)
  - Backend unreachable
  - Database connection issues
- **Test:** Trigger alerts, verify delivery

---

## Phase 8: Testing & Documentation (Days 20-21)

### 8.1 - Integration Tests (Day 20)

- [ ] Test full registration → proxy flow
- [ ] Test error scenarios (backend down, invalid fingerprint)
- [ ] Load testing (simulate 1000 concurrent connections)
- [ ] Security testing (unauthorized access attempts)
- **Test:** All tests pass with >80% coverage

### 8.2 - Documentation (Day 21)

- [ ] API documentation (OpenAPI/Swagger)
- [ ] Deployment guide
- [ ] Architecture diagram
- [ ] Runbook for operations
- [ ] User guide (how to register your Headscale instance)

---

## Minimal Viable First Version (1 Week)

If you need to ship quickly:

**Days 1-3:** Phase 1 (Basic proxy with hardcoded routes)  
**Days 4-5:** Phase 2 (Add registry)  
**Days 6-7:** Phase 6 (Basic deployment)

**Ship:** Working proxy service, manual registration via DB inserts

---

## Technology Stack

| Component      | Technology           | Why                                 |
| -------------- | -------------------- | ----------------------------------- |
| Language       | Go 1.21+             | Fast, simple, great for networking  |
| HTTP Framework | chi or net/http      | Lightweight, standard library-based |
| Database       | PostgreSQL 15        | Reliable, ACID, good for registry   |
| Cache          | In-memory (sync.Map) | Simple, no external dependency      |
| Deployment     | Docker + Fly.io      | Easy, cheap, edge deployment        |
| Monitoring     | Prometheus + Grafana | Standard observability stack        |
| DNS            | Cloudflare           | Free wildcard DNS, proxy option     |

---

## Deployment Considerations

### Scalability

- Stateless design (any instance can handle any request)
- Horizontal scaling behind load balancer
- Registry is single source of truth

### Availability

- Deploy in multiple regions (Fly.io does this automatically)
- Database replication (read replicas)
- Graceful shutdown (drain connections before restart)

### Cost

- **Low traffic:** ~$5/month (Fly.io free tier + small DB)
- **Medium traffic (1000 users):** ~$20/month
- **High traffic:** Add caching layer (Redis) to reduce DB load

---

## Success Criteria (MVP)

- [ ] Register Headscale endpoint via API
- [ ] Route traffic from `<fingerprint>.headfwd.net` to user's Headscale
- [ ] Handle 1000 concurrent connections
- [ ] 99% uptime over 1 week
- [ ] Auth prevents unauthorized registrations
- [ ] Documented deployment process

---

## What NOT to Build Yet

- Web UI for registration (API-first is fine)
- Complex analytics dashboard
- Multi-tenant accounts
- Payment/billing (if needed later)
- Custom protocol (stick with HTTP/HTTPS)
- Load balancing to multiple Headscale instances per user

---

## Next Steps

1. **Set up Go project** (`go mod init`)
2. **Build basic proxy** (Phase 1.2)
3. **Test locally** with ngrok or localhost
4. **Add registry** (Phase 2)
5. **Deploy to Fly.io** (Phase 6)

---

## Alternatives Considered

### Rust

**Pros:** Maximum performance, memory safety  
**Cons:** Steeper learning curve, slower development  
**Verdict:** Use if you need absolute max performance or have Rust expertise

### Node.js

**Pros:** Fast development, huge ecosystem  
**Cons:** Less performant for proxy, callback hell without TypeScript  
**Verdict:** Use if you're a JS expert and need rapid prototyping

### Caddy/Traefik

**Pros:** Battle-tested, plugin systems  
**Cons:** Less control, harder to customize for fingerprint routing  
**Verdict:** Consider if you want minimal custom code, but may need plugins

---

## Questions to Answer Before Starting

1. **Where will you deploy?** (Fly.io, AWS, DigitalOcean, self-hosted?)
2. **Expected scale?** (10 users? 1000? 10000?)
3. **Database preference?** (PostgreSQL vs SQLite vs Redis)
4. **Do you own headfwd.net?** (Need to set up wildcard DNS)
5. **Authentication model?** (API keys? Mutual TLS? Magic links?)

---

## Getting Started

```bash
# Initialize project
mkdir keyexchange && cd keyexchange
go mod init github.com/yourorg/keyexchange

# Create basic structure
mkdir -p cmd/server internal/{proxy,registry,config}

# Install dependencies
go get github.com/go-chi/chi/v5

# Run
go run cmd/server/main.go
```

Ready to start with Phase 1.1? 🚀
