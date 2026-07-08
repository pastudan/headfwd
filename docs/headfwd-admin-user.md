prompt:

Can you make the first user a "headfwd admin" which has the ability to view this headfwd-sidecar users, add devices, delete, etc. But everyone else should _not_ be, for obvious reasons. Maybe show a toggle on the main users page of if they are an admin? So that you can enable admin rights for other users too? not sure what the best UX is for that, lets enter plan mode for this and think it through

---

llm:

Good call to plan this — there are several non-obvious design decisions here. Let me think through the full picture before touching a line of code.
Identity & trust model
The portal already has the pieces needed. When a request arrives from the tailnet, the source IP is a 100.64.x.x address that maps to a specific headscale node → user. The /api/hello handler already does this lookup. We trust that identity because the tsnet listener only accepts authenticated WireGuard peers — you can't spoof a tailnet IP without the node's WireGuard keypair.
For local/dev access (requests from 127.x, 192.168.x, 10.x), there's no tailnet identity. Two options:
A) Bypass admin check for local access — common pattern in self-hosted tools (Gitea, Immich, etc.), show a warning banner
B) Require an env var HEADFWD_ADMIN=username as a manual bootstrap credential for local access
I lean toward A for dev UX, with the warning banner. Local access = full admin. Prod access via tailnet = must be admin user.
Where admin state lives
Headscale has no native concept of "admin". Options:
Option Pros Cons
JSON file on disk (admins.json) Simple, persists across restarts, readable Need to handle file location
Env var HEADFWD_ADMINS=alice,bob No state file Can't change at runtime
Headscale user tags Uses existing data Tags are for nodes, not users; hacky
In-memory only Simplest Resets on restart
Recommendation: JSON file at {state_dir}/admins.json (same dir as the tsnet state). Survives restarts. The AdminStore is a small in-memory struct backed by this file, with mutex for safe concurrent writes.
Bootstrap logic
Problem: Fresh install has no admins → nobody can access the portal to set up admins → deadlock.
Solution: On first portal request (or on startup) if admins.json doesn't exist or is empty, auto-promote the headscale user with the oldest createdAt timestamp. This gives the "first user" admin by default without requiring any config.
Edge case: what if there are no headscale users yet? Wait until the first user is created, then auto-promote them.
API surface
New endpoints:
GET /api/me → { user_name, is_admin, is_local_access }GET /api/admins → { admins: ["alice"] } (admin-only)PUT /api/admins/{name} → promote user to admin (admin-only)DELETE /api/admins/{name} → revoke admin (can't revoke self) (admin-only)
Protected by admin middleware (all existing management routes):
POST /api/usersDELETE /api/users/{name}POST /api/keysDELETE /api/nodes/{id}GET /api/users ← protected too (non-admins don't need to see the list)GET /api/nodes ← same
Remains unprotected:
GET /api/hello ← any tailnet peer can check their own identityGET /api/me ← any tailnet peer can check their admin status
Admin middleware logic:

1. If remote IP is local (127.x / RFC1918): bypass → local admin2. Look up headscale node by remote IP3. If user is in admin list: allow4. Else: 403 {"error": "forbidden", "hint": "ask an admin to grant you access"}
   Frontend changes
   useCurrentUser() hook
   Calls /api/me. Returns { userName, isAdmin, isLocalAccess }. Used by the main layout to gate UI.
   Access control flow
   Portal loads → fetch /api/me ├─ isAdmin = true → show full management UI (current behaviour) └─ isAdmin = false → show "Access restricted" screen with message "Contact your headfwd admin to gain access." (no management controls shown at all — simpler than degraded view)
   UserRow admin toggle
   In the users list, next to the user name:
   Admin users: a filled shield icon (blue)
   Non-admin users: no icon
   Hovering the row (same pattern as delete): shows "Make admin" / "Remove admin" link
   Clicking shows inline confirm: "Grant [alice] admin access? They will be able to manage all users and devices."
   Can't toggle yourself (disabled, tooltip: "Can't remove your own access")
   At least 1 admin must always remain (block last-admin revocation)
   Open questions to resolve before building
   Should non-admin tailnet users see the portal at all, or get an error? I suggest a simple "Access restricted" page — they can still see their own identity (name, IP) via /api/me, which is nice UX ("you're connected but not an admin").
   Should the admin toggle be on-hover (like delete) or always visible? Given it's a significant permission change, I lean toward always visible as a shield icon badge — instant visual clarity on who has admin. Clicking it opens an inline confirm.
   What happens if the headfwd-server node (the sidecar itself) is the "first" user in headscale? It would get auto-promoted as admin, which is wrong. We should exclude the headfwd-server system user from auto-bootstrap candidates.
   Should admins be able to see/manage the headfwd-server system user in the UI? Probably hide it from the list, or at least prevent deleting it.
   Recommended build order
   AdminStore (Go struct + file) + /api/me endpoint
   Admin middleware applied to all management routes
   Bootstrap logic (auto-promote oldest non-system user)
   Admin API endpoints (GET/PUT/DELETE /api/admins)
   Frontend useCurrentUser hook + access gate
   Admin badge on UserRow + grant/revoke toggle
   Decisions (resolved):
   Non-admin portal view: "Access restricted" page (simplest) — show their own identity but no management controls.
   Admin toggle visibility: always visible shield icon badge on the user row — instant visual clarity on who has admin.
   System user in UI: show headfwd-server with a "system" badge and prevent deletion (do not hide it).
