# AVSecure — Change Journal

This file is the running logbook for the project. Update it before every major commit and push.

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

## 2026-06-23 — Session 3: Author credit

Added "A Macklepenny Movement project" credit to the footer of the landing page (`client/src/components/IndexPage.jsx`). Displayed in a subtle muted style beneath the privacy note.
