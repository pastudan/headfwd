# HeadFwd Development Plan

## Overview

Build an iOS app + backend that implements the HeadFwd decentralized remote access pattern for self-hosted media.

**First implementation:** Music streaming from local library  
**Core tech:** Headscale mesh + WireGuard tunnels + REST API

---

## Development Order & Rationale

### Phase 1: Backend First (Recommended Start)

**Why:** Gives you something to test against, validates the data model, and iOS development is faster when you have real endpoints.

### Phase 2: Headscale Integration

**Why:** Can be added after basic local testing works. Start with localhost, add mesh networking later.

### Phase 3: iOS Client

**Why:** Build against working backend. Can use iOS Simulator with localhost initially.

---

## Phase 1: Backend API Server

**Goal:** Self-hosted server that indexes and serves a music library

### 1.1 - Basic HTTP Server (Day 1-2)

- [ ] Choose framework (Go/Python/Node)
- [ ] Set up project structure
- [ ] Create "Hello World" endpoint
- [ ] Add basic logging
- **Test:** `curl localhost:8080/health` returns 200

### 1.2 - Library Indexing (Day 3-4)

- [ ] Scan a directory for audio files (mp3, m4a, flac)
- [ ] Extract metadata (artist, album, title, duration)
- [ ] Store in simple data structure (SQLite)
- [ ] Create `/api/scan` endpoint to trigger indexing
- **Test:** Point at test library folder, verify metadata extraction

### 1.3 - Basic API Endpoints (Day 5-7)

- [ ] `GET /api/library` - list all tracks
- [ ] `GET /api/artists` - list artists
- [ ] `GET /api/albums` - list albums
- [ ] `GET /api/track/:id` - get track metadata
- [ ] `GET /api/stream/:id` - stream audio file
- **Test:** Use curl/Postman to fetch library, stream a file

### 1.4 - Simple Auth (Day 8)

- [ ] Generate server keypair on first run
- [ ] Basic token-based auth (single user)
- [ ] Add auth header validation to endpoints
- **Test:** Request with/without valid token

### 1.5 - Cover Art & Basic Metadata (Day 9-10)

- [ ] Extract embedded album art
- [ ] Serve cover images via `/api/cover/:id`
- [ ] Add thumbnail generation
- **Test:** Load cover art in browser

---

## Phase 2: Headscale Integration

**Goal:** Make backend accessible through Tailscale mesh

### 2.1 - Local Headscale Setup (Day 11-12)

- [ ] Install Headscale on dev machine (or Docker)
- [ ] Configure basic Headscale server
- [ ] Join backend server to mesh
- [ ] Document Headscale URL/port config
- **Test:** Backend accessible via Tailscale IP from another device

### 2.2 - Connection Bootstrapping (Day 13-14)

- [ ] Generate QR code with Headscale pubkey fingerprint
- [ ] Create `/api/mesh-info` endpoint (returns connection details)
- [ ] Document mesh joining process
- **Test:** Join second device, verify mesh connectivity

### 2.3 - STUN/DERP Configuration (Day 15)

- [ ] Configure DERP relay server (or use public)
- [ ] Set up STUN for NAT traversal
- [ ] Test direct P2P vs relay fallback
- **Test:** Connect from behind different NATs

---

## Phase 3: iOS Client

**Goal:** Native iOS app that browses and streams music

### 3.1 - Basic iOS Project Setup (Day 16)

- [ ] Create new Xcode project (Swift, SwiftUI)
- [ ] Set up basic navigation structure
- [ ] Add placeholder screens (Library, Artists, Albums, Now Playing)
- **Test:** App launches, navigation works

### 3.2 - Network Layer (Day 17-18)

- [ ] Create API client service
- [ ] Implement auth token storage (Keychain)
- [ ] Add basic error handling
- [ ] Test with hardcoded localhost URL
- **Test:** Fetch library list from backend running on Mac

### 3.3 - Library UI (Day 19-21)

- [ ] Display track list (title, artist, duration)
- [ ] Show album art thumbnails
- [ ] Add pull-to-refresh
- [ ] Implement basic search/filter
- **Test:** Browse full library, scroll performance

### 3.4 - Audio Player (Day 22-25)

- [ ] Integrate AVPlayer for streaming
- [ ] Build Now Playing UI (play/pause, seek, progress)
- [ ] Add background audio support
- [ ] Show lock screen controls
- **Test:** Stream tracks, skip, seek, background playback

### 3.5 - Playback State (Day 26-27)

- [ ] Track current position
- [ ] Persist playback state locally
- [ ] Resume where left off
- **Test:** Kill/relaunch app, verify resume works

### 3.6 - Queue Management (Day 28-29)

- [ ] Play next/previous track
- [ ] Show upcoming queue
- [ ] Basic shuffle/repeat modes
- **Test:** Queue multiple tracks, shuffle

---

## Phase 4: iOS + Headscale Integration

**Goal:** iOS app connects through Tailscale mesh

### 4.1 - Tailscale iOS Integration (Day 30-32)

- [ ] Add Tailscale SDK or use NetworkExtension
- [ ] Implement mesh connection in app
- [ ] Handle connection state (connected/disconnected)
- [ ] Store Headscale server config
- **Test:** App connects to backend via Tailscale IP

### 4.2 - QR Code Onboarding (Day 33-34)

- [ ] Add QR scanner (scan Headscale fingerprint)
- [ ] Validate and store mesh config
- [ ] Auto-connect after successful scan
- **Test:** Fresh install → scan QR → connect to backend

### 4.3 - Connection UI/UX (Day 35)

- [ ] Show connection status indicator
- [ ] Handle offline gracefully
- [ ] Add retry logic
- **Test:** Toggle wifi, verify reconnection

---

## Phase 5: Polish & Testing

### 5.1 - Error Handling (Day 36-37)

- [ ] Network error states
- [ ] Empty states (no library)
- [ ] Loading indicators
- **Test:** Airplane mode, slow network, empty library

### 5.2 - Performance (Day 38-39)

- [ ] Optimize cover art loading (caching)
- [ ] Test with large library (1000+ tracks)
- [ ] Memory profiling
- **Test:** Large library scroll, memory usage

### 5.3 - Documentation (Day 40-42)

- [ ] Backend setup guide
- [ ] iOS build instructions
- [ ] User guide (how to deploy)
- [ ] API documentation

---

## Recommended Starting Order

### Option A: Backend → iOS → Headscale (Recommended)

**Pros:** Test locally first, iterate faster, add networking last  
**Path:**

1. Build backend with local file access
2. Test with curl/Postman
3. Build iOS app against localhost
4. Add Headscale once everything works locally

### Option B: Headscale → Backend → iOS

**Pros:** Networking solved upfront  
**Cons:** Harder to debug, slower iteration  
**Use if:** You already have Headscale experience

---

## Minimal Viable First Version

If you want to ship something testable quickly (1-2 weeks):

**Week 1:**

- Backend: Library indexing + streaming endpoints
- Test with curl

**Week 2:**

- iOS: List view + basic player
- Test on localhost

**Ship:** Working prototype, document "Headscale integration coming soon"

---

## Technology Recommendations

### Backend

- **Go:** Fast, single binary, good for self-hosting
- **Python (FastAPI):** Rapid development, great libraries (mutagen for metadata)
- **Node.js:** If you know JS, works well

### iOS

- **SwiftUI:** Modern, less boilerplate
- **UIKit:** If you need more control over player UI

### Database

- **SQLite:** Perfect for single-user, embedded
- **PostgreSQL:** If planning multi-user later

---

## Success Criteria (MVP)

- [ ] Backend indexes local music directory
- [ ] iOS app displays library
- [ ] Stream and play audio end-to-end
- [ ] Works over Tailscale mesh
- [ ] Single user can onboard via QR code
- [ ] Documented setup process

---

## What NOT to Build Yet

- Multiple users / accounts
- Playlists / favorites
- Lyrics / rich metadata
- Equalizer / DSP
- Offline downloads
- Cross-device sync
- Social features
- tvOS / macOS clients

Focus: **One user, one library, one iOS device, streaming only**

---

## Next Steps

1. **Choose your backend language/framework**
2. **Set up basic project structure**
3. **Start with Phase 1.1 - Basic HTTP Server**
4. **Test each step before moving forward**

Questions to answer before starting:

- What backend language are you most comfortable with?
- Do you have a test music library ready?
- Do you have an Apple Developer account (for device testing)?
- Where will you deploy the backend first? (Local machine, Raspberry Pi, VPS?)
