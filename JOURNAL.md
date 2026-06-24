# AVSecure — Change Journal

---

## 2026-06-24 — Session 11: Server hardening

### Summary

Full security audit of public-facing server. Seven concrete gaps closed.

### 1. Firewall (UFW)

Previous state: **default policy was ALLOW** — the firewall was effectively off, every port was internet-accessible.

New state: `default deny incoming`. Explicit allow list:

| Port(s) | Access | Purpose |
|---|---|---|
| 22/tcp | Anywhere | SSH |
| 80/tcp | Anywhere | HTTP → HTTPS redirect |
| 443/tcp | Anywhere | HTTPS |
| 8443/tcp | Anywhere | WebSocket signaling |
| Samba (137/138/139/445) | 192.168.0.0/24 only | LAN file sharing |
| 3000, 8000, 8060, 4000 | 192.168.0.0/24 only | Dev ports, LAN only |
| 5353/udp | 192.168.0.0/24 only | mDNS, LAN only |

### 2. Go server bind address

Was `*:4242` (all interfaces, internet-accessible, no TLS). Now `127.0.0.1:4242` — loopback only, reachable only through nginx's TLS proxy.

### 3. Dedicated service user

Created `avsecure` system user (no login shell, no home dir, no sudo). Binary moved to `/opt/avsecure/go-react-webrtc`. Systemd hardening directives added: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `CapabilityBoundingSet=`.

Deploy workflow: `./deploy-server.sh` (builds, copies to /opt/avsecure/, restarts service).

### 4. HTTP server timeouts (Slowloris mitigation)

Added to `main.go`: `ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`, `IdleTimeout: 120s`.

### 5. WebSocket write deadline

`Broadcaster` goroutine now sets `SetWriteDeadline(now + 10s)` before every `WriteJSON` call. Previously a single unresponsive client could stall message delivery to all other participants indefinitely.

### 6. Room count cap (DoS mitigation)

`var maxRooms = 1000` in `rooms.go`. `AtCapacity()` method on `RoomMap`. `/create` handler returns HTTP 503 when at capacity. TDD: 3 new tests.

### 7. nginx hardening

- `server_tokens off` (hides nginx version)
- `ssl_protocols TLSv1.2 TLSv1.3` + modern cipher suite
- Security headers: HSTS (2-year, preload), X-Frame-Options: DENY, X-Content-Type-Options, Referrer-Policy, Permissions-Policy, Content-Security-Policy
- HTTP → HTTPS redirect on port 80
- Rate limiting: `/create` 5/min burst 3, `/join` 30/min burst 10 (per source IP)
- gzip enabled

---

## 2026-06-24 — Session 9: CRA → Vite migration (0 vulnerabilities)

### Build tool replaced: react-scripts → Vite

Migrated from Create React App (`react-scripts@5.0.1`, abandoned since 2022) to Vite 6.

**Result:** 100 → 0 vulnerabilities. 1,223 packages removed; 59 added. dep tree went from ~1,400 to ~286 packages.

**Files changed:**
- `client/vite.config.js` — new; configures `@vitejs/plugin-react`, `outDir: 'build'` (preserves deploy script), vitest with `jsdom` + `globals: true`
- `client/index.html` — moved from `public/index.html` to project root; removed `%PUBLIC_URL%` prefix from all asset hrefs; added `<script type="module" src="/src/index.jsx">` entry point
- `client/package.json` — removed `react-scripts`, `web-vitals`, `@babel/runtime`, all overrides, `jest` section, `eslintConfig` section; added `vite`, `@vitejs/plugin-react`, `vitest`, `jsdom`; scripts: `start → vite`, `build → vite build`, `test → vitest run`
- `client/src/components/Rooms.jsx` — `process.env.REACT_APP_*` → `import.meta.env.VITE_*`
- `client/src/components/Rooms.test.jsx` — `jest.fn()` → `vi.fn()` (vitest globals)
- `client/build.sh` — `REACT_APP_*` → `VITE_*` throughout the credential guard
- `client/.env.example`, `client/.env`, `/etc/avsecure/secrets`, `README.md` — `REACT_APP_*` → `VITE_*`

**Test runner replaced:** Jest (via react-scripts) → Vitest 3.x. API is Jest-compatible; only change in tests was `jest.fn()` → `vi.fn()` (2 occurrences). All 12 tests pass.

**Build time:** CRA ~30–60s → Vite ~2s.

**Why the overrides block was removed entirely:** All 38 overrides targeted packages that came from `react-scripts`' webpack stack (sockjs, webpack-dev-server, svgo, postcss-old, etc.). None of those packages exist in Vite's dep tree. Running `npm audit` after the migration: `found 0 vulnerabilities`.

**Env var rename:** CRA's `REACT_APP_*` convention (build-time `process.env` injection) replaced by Vite's `VITE_*` convention (accessed as `import.meta.env.VITE_*`). Updated in all credential files including the live `/etc/avsecure/secrets`.

---

This file is the running logbook for the project. Update it before every major commit and push.

---

## 2026-06-24 — Session 10 (cont): Input validation hardening

### Server-side input guards (`server/signaling.go`)

Three gaps closed:

1. **Nickname length** — `sanitizeNickname(s)`: trims whitespace, truncates to 24 runes (not bytes — correct for multi-byte Unicode/emoji). Client-side `maxLength={24}` was already enforced in the browser; this adds the server-side equivalent so a crafted client can't inject arbitrarily long nicknames.

2. **WebSocket message size** — `ws.SetReadLimit(65536)` (64 KB) set immediately after upgrade. A single SDP is at most ~15 KB; 64 KB gives 4× headroom while making message-bomb attacks impossible.

3. **Relay type allowlist** — `isRelayableType(t)` returns true only for `offer`, `answer`, `iceCandidate`. The relay loop drops any other type silently. Prevents a crafted client from injecting forged `leave`, `roster`, `join`, or arbitrary messages to other participants.

**TDD:** 6 new server tests (`TestSanitizeNickname_*`, `TestIsRelayableType_*`); all pass with `-race`.

---

## 2026-06-24 — Session 10: Persistent rooms with activity-based TTL + client heartbeat

### Persistent rooms

Rooms now survive after all participants leave. Previously, a room was deleted the moment its last participant disconnected — requiring everyone to stay in the call to keep it alive. Now:

- Rooms persist for **4 hours of inactivity** after the last participant leaves
- Any WebSocket message (offer, answer, ICE, ping) resets the 4-hour clock
- A background goroutine sweeps expired empty rooms every 15 minutes
- `InsertIntoRoom` rejects joins to expired rooms (server sends WebSocket close)

**Server changes (`server/rooms.go`):**
- `var roomTTL = 4 * time.Hour` (var not const — overridable in tests)
- `RoomMap` gains `expiresAt map[string]time.Time` alongside `Map`
- `Init()` initialises both maps
- `CreateRoom()` sets TTL on creation
- `InsertIntoRoom()` checks expiry (empty rooms only) and extends TTL on join
- `RemoveFromRoom()` resets TTL on empty instead of deleting the room
- New `Touch(roomID)` method — resets TTL to `now + 4h`
- New `SweepExpired()` method — deletes expired empty rooms

**Server changes (`server/signaling.go`):**
- `AllRooms.Touch(roomID)` called on every message in the relay loop
- `ping` messages handled specially: Touch + send `pong`, not relayed to other peers

**Server changes (`main.go`):**
- Sweep goroutine: `SweepExpired()` every 15 minutes

### Client heartbeat (`client/src/components/Rooms.jsx`)

- `setInterval` sends `{type: "ping"}` every 30 seconds while the WebSocket is open
- Interval cleared on component unmount (hang up / navigate away)
- As long as any tab is open and in a room, the room stays alive indefinitely
- Server `pong` response is silently ignored (used for future connection health monitoring if needed)

### Behaviour summary

| Scenario | Result |
|---|---|
| Active call (offers, ICE, answers flowing) | Room alive (messages Touch TTL) |
| Tab open, no call | Room alive (ping every 30s Touches TTL) |
| All tabs closed after call | Room alive for 4 more hours |
| 4h with no pings and no participants | Room swept and deleted |
| Rejoin after everyone left (within 4h) | Works — empty room still exists |
| Try to join after 4h expiry | Server rejects (WebSocket close with error) |

### TDD

7 new server tests (all pass, `-race`): `TestRemoveFromRoom_PersistsWhenEmpty`, `TestRoom_CanRejoinAfterLeaving`, `TestInsertIntoRoom_RejectsExpiredRoom`, `TestSweepExpired_RemovesExpiredEmptyRoom`, `TestSweepExpired_KeepsActiveRoom`, `TestSweepExpired_KeepsNonExpiredRoom`, `TestTouch_ExtendsRoomTTL`. Renamed `TestRemoveFromRoom_DeletesEmptyRoom` → `TestRemoveFromRoom_PersistsWhenEmpty`.

2 new client tests: `pong` handled silently; join message includes nickname.

---

## 2026-06-22 — Session 1: Infra restoration, UI overhaul, bug fixes, stats, tests

### Infrastructure

- **GitHub**: Pushed project to `https://github.com/robinmack/webrtc-chat`. Fixed a broken remote URL (`https://github.com/robinmack` with no repo name). Removed accidentally committed `node_modules` from the initial commit.
- **Domain**: Confirmed `avsecure.vip` is registered and active via Cloudflare (account: writer.robin.mack@gmail.com).
- **SSL**: Existing Cloudflare origin certificate still valid. nginx configured to present it.
- **ISP change**: Server moved behind AT&T Fiber. Required:
  - AT&T BGW320 IP Passthrough → TP-Link BE800 WAN
  - TP-Link port forwarding TCP+UDP 20–9000 → 192.168.0.201
  - Cloudflare DNS A-record updated to new public IP
- **systemd service**: Created `/etc/systemd/system/avsecure.service` — runs the Linux binary, restarts on failure, starts on boot. Binary compiled as `go-react-webrtc-linux` (GOOS=linux GOARCH=amd64).
- **nginx**: Reverse proxy on port 8443 → 4242 (Go server), WebSocket upgrade headers in place.

### WebRTC bug fixes

- **Broadcaster race**: `go broadcaster()` was called once per WebSocket connection, spawning multiple competing goroutines. Fixed by moving `go server.Broadcaster()` to `main.go` (called once at startup).
- **Mutex data race**: Two competing mutexes (`AllRooms.Mutex` and a package-level `mu`) were both guarding the same map. Replaced with a single `sync.RWMutex` on `RoomMap`.
- **No participant cleanup**: Disconnecting clients left zombie entries in `AllRooms`, blocking reconnection. Fixed with `defer AllRooms.RemoveFromRoom(roomID, ws)` in `JoinRoomRequestHandler`. Empty rooms are now auto-deleted.
- **Reoffer flicker (~20s)**: The periodic reoffer timeout was only cleared on the callee side (on answer received). Fixed by also clearing it in `onconnectionstatechange` when `connectionState === 'connected'`.
- **Ambiguous room IDs**: Characters `0`, `O`, `1`, `l`, `I` were causing transcription errors (e.g. `Ql7` read as `Q17`). Removed from `roomIDChars` in `generateRoomID()`.

### UI modernisation

- Replaced plain HTML with **Tailwind CSS v3** (`darkMode: 'class'`).
- **Dark mode default**, with a light/dark toggle (sun/moon icons) persisted to `localStorage`.
- Clean header: lock icon, "AVSecure" brand, ALPHA badge, theme toggle.
- Landing page: hero text, two-card layout (Create Room / Join Room).
- Room page: room ID bar with Copy Link + QR buttons, call controls (camera/mic/hang up), "End-to-end encrypted" footer.

### Layout (room page)

- Initially implemented PiP (partner fills frame, self-view overlaid in a moveable corner) and side-by-side (flex with 1:1 / 2:1 / 3:1 ratio options). These were later replaced by the dynamic grid (see Session 2).

### Anonymous statistics

Added a persistent SQLite stats system (`server/stats.go`):
- **WAL mode**, `SetMaxOpenConns(1)` for write serialisation.
- Tables: `counters` (visits, rooms_created) and `chat_sessions` (duration_seconds, created_at).
- Endpoints: `POST /stats/visit`, `POST /stats/chat`, `GET /stats/public`.
- `PublicStats` struct: visitor count, rooms created, chats total/week/month/year, average duration per period, total minutes.
- Frontend stats modal (bar-chart icon in header):
  - Three headline tiles: Visitors / Rooms created / Total minutes.
  - Table: Chats + Avg duration for This week / This month / This year / All time.
  - Security blurb about DTLS-SRTP encryption (see below).
- Visit recorded on every page load; chat duration recorded in `Rooms.jsx` on hang-up (only if a real WebRTC connection was established).

### Security

DTLS-SRTP blurb added to the stats modal:
> All video and audio is encrypted with DTLS-SRTP — the same standard used by Signal and WhatsApp. Encryption keys are negotiated directly between browsers and never touch our server. We store no video, audio, IP addresses, or room IDs. The only data we record is call count and duration.

### QR code / share

- "QR" button in the room info bar opens a modal with a scannable QR code for the room URL.
- "Save QR" downloads the QR as a PNG (canvas-based via `qrcode.react`).
- "Copy link" also available inside the QR modal.
- QR always has white background so it scans correctly in dark mode.

### Unit tests (`server/`)

55 tests, all passing with `-race`:

| File | What's covered |
|---|---|
| `rooms_test.go` | Room ID generation (length, charset, uniqueness, no ambiguous chars), `CreateRoom`, `InsertIntoRoom` (success / not-found / full / duplicate), `RemoveFromRoom` (partial / last-participant / ghost room), `Get`, `GetParticipantIDs`, concurrent create+get |
| `stats_test.go` | `InitStats` idempotency, counter seeding, `RecordVisit/Room/Chat`, all three HTTP handlers (status codes, CORS headers, reflected data, nil-DB 503) |
| `signaling_test.go` | `isValidRoomID` (valid, too short, too long, boundary, invalid chars), `CreateRoomRequestHandler` (JSON body, CORS, OPTIONS, counter increment, unique IDs), `JoinRoomRequestHandler` validation layer |

---

## 2026-06-23 — Session 2: Multi-participant, BETA, logo, cleanup

### Multi-participant rooms (up to 8, full-mesh P2P)

**Architecture decision:** Full-mesh P2P — each participant opens N-1 peer connections directly to every other participant. No media relay server. Practical ceiling: 6–8 participants (browser CPU + upload bandwidth). Beyond 8, an SFU (Janus, LiveKit, etc.) would be needed.

**Theoretical connection count:**

| Participants | Connections per peer | Network total |
|---|---|---|
| 2 | 1 | 1 |
| 4 | 3 | 6 |
| 6 | 5 | 15 |
| 8 | 7 | 28 |

**Protocol changes** (new message types added to the WS signaling channel):

| Message | Direction | Purpose |
|---|---|---|
| `roster` | server → new joiner | List of existing peer IDs on join |
| `join` | server → existing peers | Notifies that a new peer arrived |
| `leave` | server → remaining peers | Notifies that a peer disconnected |
| `offer / answer / iceCandidate` | peer → peer (targeted via `from`/`to` fields) | WebRTC handshake |

**Invariant:** the new joiner always sends offers to all existing peers; existing peers only respond. This prevents the "who calls who" race condition.

**Backend changes:**
- `server/rooms.go`: `maxParticipantsPerRoom` raised to 8; `PeerID string` added to `Participant` struct; `InsertIntoRoom` now accepts `peerID`; `GetParticipantIDs(roomID)` helper added.
- `server/signaling.go`: `BroadcastMsg` gains `From`/`To` fields; `Broadcaster()` routes targeted messages by `PeerID`; `JoinRoomRequestHandler` reads initial join handshake, snapshots existing peer IDs, sends roster, broadcasts join notification, relays `from`/`to` through the message loop, and broadcasts leave on disconnect.

**Frontend changes (`client/src/components/Rooms.jsx`):**
- Replaced single `peerRef` with `peersRef` Map (remotePeerId → RTCPeerConnection).
- Replaced module-level `reofferTimeout` with `reofferTimers` Map (per-peer).
- Replaced single `iceCandidateCount` with `icCounts` Map (per-peer).
- Replaced single `partnerVideo` ref with `remoteStreams` state Map (peerId → MediaStream), triggering re-renders only on participant changes.
- Added `createPeerFor(remotePeerId)`, `handleNegotiationNeededFor`, `handleIceCandidateFor`, `callPeer`, `handleOfferFrom`, `closePeer`, `closeAllPeers`.
- Removed the PiP / side-by-side layout system entirely.
- **Dynamic CSS Grid** — auto-scales columns: 1 peer = 1 col; 2–4 = 2 col; 5–6 = 3 col; 7–8 = 4 col.
- `hangUp` closes all N-1 peer connections before navigating away.
- Footer updated: "Up to 8 participants".

**Tests updated:** All `InsertIntoRoom` call sites given peer ID strings; 3 new `GetParticipantIDs` tests added; `TestInsertIntoRoom_RoomFull` still uses `maxParticipantsPerRoom` as loop bound (now spins 9 WS connections).

### BETA badge

`client/src/App.jsx`: header badge changed from `ALPHA` → `BETA`.

### Social sharing logo

- Created `client/public/logo.svg` and `client/public/favicon.svg`: abstract whisper logo — two geometric heads, left leaning toward right, three curved arcs suggesting a whisper, dark-blue background disc.
- Generated `logo192.png` and `logo512.png` from the SVG using Inkscape.
- Updated `client/public/index.html`:
  - SVG favicon (`<link rel="icon" type="image/svg+xml">`), ICO kept as fallback.
  - Open Graph meta tags (`og:title`, `og:description`, `og:image`, `og:url`, `og:type`).
  - Twitter card meta tags.
  - Page title updated to "AVSecure — Encrypted Video Chat".
  - `theme-color` updated to `#1e3a5f` (matching the logo background).

### Cleanup

- Removed CRA boilerplate: `reportWebVitals.js`, `setupTests.js`.
- Removed `reportWebVitals` import and call from `index.jsx`.
- Added `*.bak` and `*.orig` to `.gitignore`.

---

### License

Added `LICENSE` (MIT, Copyright 2026 Robin Macklepenny). Chosen for maximum permissiveness — anyone can use, modify, and redistribute the code with attribution — while retaining the standard "AS IS" warranty disclaimer for legal protection.

---

---

## 2026-06-23 — Session 5: Public repo, Macklepenny logo, teal palette

### Public GitHub repo

Renamed `robinmack/avsecure` (private, dirty history) to `robinmack/avsecure-private`. Created a fresh `robinmack/avsecure` public repo with a single squashed commit — no old TURN credentials in history. Working directory remote updated to point at the new public repo.

### Macklepenny Movement logo

Added `client/public/macklepenny-movement.svg` (bridge icon + wordmark + "Building bridges together" motto). Displayed in the footer of both `IndexPage.jsx` and `Rooms.jsx` with a subtle opacity hover effect.

### Teal colour palette

Replaced all `blue-*` Tailwind classes with `teal-*` across `App.jsx`, `IndexPage.jsx`, and `Rooms.jsx` to match the deep teal (`#022f33`) of the Macklepenny Movement logo. Updated `theme-color` meta tag in `index.html` from `#1e3a5f` to `#022f33`.

---

## 2026-06-23 — Session 4: TURN credentials, README, open-source prep

### Security: TURN credentials moved out of source

The metered.ca TURN credentials were hardcoded in `client/src/components/Rooms.jsx`. Moved to environment variables so they are never committed:

- `REACT_APP_TURN_USERNAME` and `REACT_APP_TURN_CREDENTIAL` read via `process.env` at build time (CRA convention).
- TURN servers are only added to `ICE_SERVERS` if both vars are present; app falls back to STUN-only if they are not set.
- Created `client/.env.example` as a committed template with instructions and links to metered.ca, Twilio, and Xirsys.
- Added `client/.env` (real credentials) to root `.gitignore`.
- Also added `go-react-webrtc-linux` (compiled binary) and `stats.db` (live data) to `.gitignore` — both were previously untracked and at risk of being accidentally committed.

### README rewritten

The original README was a scratchpad with an expired cert date and outdated notes. Replaced with a proper project README covering: what the app does, the stack, TURN setup instructions, local dev steps, production build and nginx config, project structure, and how to run the tests.

---

## 2026-06-23 — Session 7: Secure secrets storage

### `/etc/avsecure/secrets` (layered credential lookup)

TURN credentials were previously stored only in `client/.env` (gitignored but world-readable by default). Moved to a system-level file outside the project tree with restrictive permissions (`chmod 600`, owned by the deploy user). `client/.env` is retained as a fallback for local development.

`build.sh` now checks in priority order:
1. `/etc/avsecure/secrets` — recommended for production servers
2. `client/.env` — fallback for local dev

Error message clearly explains both options if neither file is found.

Setup for new server deployments:
```bash
sudo mkdir -p /etc/avsecure
sudo cp client/.env.example /etc/avsecure/secrets
sudo chmod 600 /etc/avsecure/secrets
sudo chown $(whoami):$(whoami) /etc/avsecure/secrets
```

README updated with both options under "Configure the frontend."

---

## 2026-06-23 — Session 8: Nickname system

### Nickname entry screen (pre-join gate)

Users now pick a nickname before entering any room. Joining is gated — the WebSocket does not connect until a name is confirmed.

**UI:**
- Clean centered card with text input, 🎲 Randomize button, and "Join as [name]" button.
- Randomize picks from a list of 65 curated nouns (animals, foods, occupations).
- "Join as [name]" disabled until the input has content; pressing Enter also works.
- The room is never loaded (no camera, no WebSocket) until the user confirms.

**Signaling changes (protocol update):**
- `join` message: `{ type: "join", peerId, nickname }` — client includes the nickname.
- `roster` entries: `{ peerId, nickname }` objects instead of bare ID strings.
- `join` broadcast: `{ type: "join", peerId, nickname }` — existing peers store the name on arrival.

**Display:**
- Each video tile shows the participant's nickname in a gradient header overlay.
- Self-view shows "[nickname] (you)".
- Nicknames are stored per peer and cleaned up when a peer leaves.

**Server changes (`server/rooms.go`, `server/signaling.go`):**
- `Participant` struct gains `Nickname string`.
- `InsertIntoRoom` takes `nickname string` as a new parameter.
- New `ParticipantInfo` struct and `GetParticipantInfo()` method return `{peerId, nickname}` slices.
- `JoinRoomRequestHandler` extracts nickname from join message (defaults to "Anonymous" if absent for backward compat), uses `GetParticipantInfo` for the roster response, includes nickname in the join broadcast.

**TDD:** Tests written first; all 12 client tests pass, all server tests pass (`go test ./server/... -race`).

### Diagnostic logging left in place

`[AV]` console logging added in Session 7 is still present to help diagnose the 3-participant visibility bug. Will be removed once that bug is resolved.

---

## 2026-06-23 — Session 6: Offer-glare fix (3-participant bug)

### Root cause

When a third peer (C) joined a room where A and B were already connected, all three peers dropped to seeing only themselves. Two compounding bugs:

1. **Spurious re-offer from answerer.** In `handleOfferFrom`, calling `addTrack` on the answerer's peer connection fires `onnegotiationneeded`. After `setLocalDescription(answer)` returns the signaling state to `'stable'`, this queued event fires `handleNegotiationNeededFor`, which sends a fresh offer back to the original offerer — the "offer glare" anti-pattern.

2. **`createPeerFor` discards working connections.** When C received A's spurious re-offer, `handleOfferFrom` called `createPeerFor(A_uuid)` unconditionally. This overwrote and orphaned the already-established C↔A peer connection, invalided in-flight ICE candidates, and caused all three participants to lose their streams.

### Fix (TDD — tests written first, then implementation)

Two new failing tests:
- `answerer never sends a re-offer after answering (no spurious onnegotiationneeded)` — verified A sends no offer after answering C
- `re-offer from remote peer reuses existing connection instead of discarding it` — verified the same `RTCPeerConnection` instance is used on re-offer

Implementation changes in `Rooms.jsx`:

- **`handleNegotiationNeededFor`**: Added `peer.signalingState !== 'stable'` guard (two checks — before and after the async `createOffer`) to prevent offers during active negotiation.
- **`handleOfferFrom`**: Reuses `peersRef.current.get(remotePeerId)` if a connection already exists. On first offer (`!existing`), sets `peer.onnegotiationneeded = null` to suppress spurious counter-offers from the answerer side, and calls `addTrack`. On re-offers, skips both (tracks already present, no new negotiation trigger).

All 7 tests pass after the fix.

---

## 2026-06-23 — Session 3: Author credit

Added "A Macklepenny Movement project" credit to the footer of the landing page (`client/src/components/IndexPage.jsx`). Displayed in a subtle muted style beneath the privacy note.
