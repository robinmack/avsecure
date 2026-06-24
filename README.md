# AVSecure

**Peer-to-peer, end-to-end encrypted video chat. No accounts. No data stored.**

Live at [avsecure.vip](https://avsecure.vip) · MIT License · A [Macklepenny Movement](https://github.com/macklepenny-movement) project

---

## How it works

- Browsers connect directly to each other using **WebRTC** — media never passes through the server.
- All audio and video is encrypted with **DTLS-SRTP** (the same standard used by Signal and WhatsApp).
- The Go server is a lightweight **signaling relay only** — it passes connection metadata (SDP offers/answers, ICE candidates) so peers can find each other, then gets out of the way.
- Up to **8 participants** per room using a full-mesh peer-to-peer topology.
- The only data recorded: visit count, room count, and anonymous call durations (no IPs, no content).

---

## Stack

| Layer | Technology |
|---|---|
| Signaling server | Go 1.25, `gorilla/websocket` |
| Database | SQLite (via `modernc.org/sqlite`, pure Go — no CGo) |
| Frontend | React 18, Tailwind CSS v3 |
| Hosting | nginx reverse proxy + TLS termination |
| DNS / CDN | Cloudflare |

---

## Prerequisites

- Go 1.21+
- Node.js 18+ and npm
- nginx (for production)
- A TURN server account (see below)

---

## TURN server setup

WebRTC uses STUN servers to discover public IP addresses, and **TURN servers** to relay traffic when peers are behind strict NAT (common on corporate networks, some mobile carriers, and double-NAT home setups).

**Without TURN credentials the app still works**, but calls may fail for some users on restrictive networks. STUN-only is fine for most home broadband connections.

### Get free TURN credentials (metered.ca)

1. Create a free account at **[metered.ca](https://www.metered.ca/tools/openrelay/)**
2. In the dashboard, go to **TURN Credentials**
3. Copy your **username** and **credential**

Other providers: [Twilio](https://www.twilio.com/stun-turn), [Xirsys](https://xirsys.com)

### Configure the frontend

Credentials are baked into the JavaScript bundle at build time by CRA. `build.sh` checks two locations in order:

**Option A — production server (recommended)**

Store credentials outside the project tree, owned by your deploy user only:

```bash
sudo mkdir -p /etc/avsecure
sudo cp client/.env.example /etc/avsecure/secrets
sudo chmod 600 /etc/avsecure/secrets
sudo chown $(whoami):$(whoami) /etc/avsecure/secrets
```

Edit `/etc/avsecure/secrets`:

```
VITE_TURN_USERNAME=your_turn_username_here
VITE_TURN_CREDENTIAL=your_turn_credential_here
```

`build.sh` sources this file automatically when it exists. No risk of the file appearing in git history regardless of future `.gitignore` changes.

**Option B — local development**

```bash
cp client/.env.example client/.env
```

Edit `client/.env` with your credentials. `client/.env` is gitignored — credentials will not be committed. `build.sh` falls back to this file when `/etc/avsecure/secrets` is absent.

---

## Running locally

```bash
# 1. Start the signaling server
go run main.go

# 2. In a separate terminal, start the React dev server
cd client
npm install
npm start
```

The Go server listens on `:4242`. The React dev server proxies API calls automatically.

---

## Building for production

```bash
# Build the Go binary
GOOS=linux GOARCH=amd64 go build -o go-react-webrtc-linux .

# Build the frontend
cd client
npm run build

# Copy frontend to your web root
sudo cp -r build/* /var/www/html/
```

### nginx config (relevant excerpts)

```nginx
# WebSocket proxy for the signaling server
location /join {
    proxy_pass         http://127.0.0.1:4242;
    proxy_http_version 1.1;
    proxy_set_header   Upgrade    $http_upgrade;
    proxy_set_header   Connection "upgrade";
}

# Signaling REST endpoints
location ~ ^/(create|join|stats) {
    proxy_pass http://127.0.0.1:4242;
}
```

### systemd service

A ready-to-use unit file is included at `avsecure.service`. Install it with:

```bash
sudo cp avsecure.service /etc/systemd/system/
sudo systemctl enable --now avsecure
```

The service expects the compiled binary at the project root and runs as the `ratwood` user — edit `User=` in the unit file to match your system.

---

## Project structure

```
├── main.go                  # Entry point: HTTP routes, startup
├── server/
│   ├── rooms.go             # Room map, participant management
│   ├── signaling.go         # WebSocket handler, message routing
│   ├── stats.go             # Anonymous SQLite stats
│   └── *_test.go            # 55 unit tests (run with: go test ./server/... -race)
├── client/
│   ├── public/              # Static assets, favicon, OG images
│   ├── src/
│   │   ├── App.jsx          # Root: theme toggle, stats modal
│   │   └── components/
│   │       ├── IndexPage.jsx  # Landing page
│   │       └── Rooms.jsx      # Video room (WebRTC, dynamic grid)
│   └── .env.example         # Template for TURN credentials
├── avsecure.service         # systemd unit file
├── JOURNAL.md               # Change log
└── LICENSE                  # MIT
```

---

## Running the tests

```bash
go test ./server/... -race -v
```

55 tests covering room management, signaling validation, stats handlers, and CORS behaviour.

---

## License

MIT — see [LICENSE](LICENSE). Copyright 2026 Robin Macklepenny / Macklepenny Movement.
