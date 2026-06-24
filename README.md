# AVSecure

**Peer-to-peer, end-to-end encrypted video chat. No accounts. No data stored.**

Live at [avsecure.vip](https://avsecure.vip) · MIT License · A [Macklepenny Movement](https://github.com/macklepenny-movement) project

---

## How it works

- Browsers connect directly to each other using **WebRTC** — media never passes through the server.
- All audio and video is encrypted with **DTLS-SRTP** (the same standard used by Signal and WhatsApp).
- The Go server is a lightweight **signaling relay only** — it passes connection metadata (SDP offers/answers, ICE candidates) so peers can find each other, then gets out of the way.
- Up to **8 participants** per room using a full-mesh peer-to-peer topology.
- Rooms persist for **4 hours of inactivity** — participants can rejoin after dropping without creating a new room. Active rooms stay alive indefinitely via a 30-second client heartbeat.
- The only data recorded: visit count, room count, and anonymous call durations (no IPs, no content).

---

## Stack

| Layer | Technology |
|---|---|
| Signaling server | Go 1.21+, `gorilla/websocket` |
| Database | SQLite (`modernc.org/sqlite`, pure Go — no CGo) |
| Frontend | React 18, Vite 6, Tailwind CSS v3 |
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

WebRTC uses STUN to discover public IP addresses, and **TURN servers** to relay traffic when peers are behind strict NAT (common on corporate networks, some mobile carriers, and double-NAT home setups).

**Without TURN credentials the app still works**, but calls may fail for some users on restrictive networks. STUN-only is fine for most home broadband connections.

### Get free TURN credentials

1. Create a free account at **[metered.ca](https://www.metered.ca/tools/openrelay/)**
2. In the dashboard, go to **TURN Credentials**
3. Copy your **username** and **credential**

Other providers: [Twilio](https://www.twilio.com/stun-turn), [Xirsys](https://xirsys.com)

### Configure the frontend

Vite injects TURN credentials into the JavaScript bundle at build time. `build.sh` reads them from one of two places:

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

`build.sh` sources this file automatically when it exists. No risk of the file appearing in git history.

**Option B — local development**

```bash
cp client/.env.example client/.env
```

Edit `client/.env` with your credentials. `client/.env` is gitignored. `build.sh` falls back to this file when `/etc/avsecure/secrets` is absent.

---

## Running locally

```bash
# 1. Start the signaling server (listens on localhost:4242)
go run main.go

# 2. In a separate terminal, start the Vite dev server (localhost:5173)
cd client
npm install
npm start
```

Open `http://localhost:5173`. The browser connects to the Go server directly on `localhost:4242`.

> **No proxy needed.** Unlike CRA, Vite does not proxy API calls by default. Both servers must be running.

---

## Building for production

### Frontend

```bash
cd client
bash build.sh
```

`build.sh` reads TURN credentials, runs `vite build`, and copies `build/` to `/var/www/html/`.

### Server binary

```bash
./deploy-server.sh
```

This builds the Go binary, copies it to `/opt/avsecure/`, and restarts the systemd service.

To build only (e.g. cross-compiling on a Mac):

```bash
GOOS=linux GOARCH=amd64 go build -o go-react-webrtc-linux .
# Then scp go-react-webrtc-linux to the server and run deploy-server.sh
```

---

## Production setup

### Service user

The server runs as a dedicated `avsecure` system user with no login shell and no sudo access:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin avsecure
sudo mkdir -p /opt/avsecure
sudo chown avsecure:avsecure /opt/avsecure
```

### systemd service

```bash
sudo cp avsecure.service /etc/systemd/system/
sudo systemctl enable --now avsecure
```

The unit file (`avsecure.service`) expects the binary at `/opt/avsecure/go-react-webrtc` and includes systemd hardening directives (`NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`).

### nginx

The signaling server binds to `127.0.0.1:4242` (loopback only). nginx handles TLS and proxies:

```nginx
# Rate limiting (defined in nginx.conf http block)
limit_req_zone $binary_remote_addr zone=create:10m rate=5r/m;
limit_req_zone $binary_remote_addr zone=join:10m   rate=30r/m;

# HTTP → HTTPS redirect
server {
    listen 80;
    server_name avsecure.vip;
    return 301 https://$host$request_uri;
}

# HTTPS frontend (port 443)
server {
    listen 443 ssl;
    ssl_protocols TLSv1.2 TLSv1.3;
    # ... security headers: HSTS, CSP, X-Frame-Options, etc.
    root /var/www/html;
    location / { try_files $uri /index.html; }
}

# WebSocket + API (port 8443)
server {
    listen 8443 ssl;
    location /create {
        limit_req zone=create burst=3 nodelay;
        proxy_pass http://127.0.0.1:4242;
    }
    location / {
        limit_req zone=join burst=10 nodelay;
        proxy_pass         http://127.0.0.1:4242;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_read_timeout 86400;
    }
}
```

### Firewall (UFW)

```bash
sudo ufw default deny incoming
sudo ufw allow 22/tcp   # SSH
sudo ufw allow 80/tcp   # HTTP redirect
sudo ufw allow 443/tcp  # HTTPS
sudo ufw allow 8443/tcp # WebSocket
# Samba and dev ports: restrict to LAN subnet
sudo ufw allow from 192.168.0.0/24 to any port 139,445 proto tcp
sudo ufw allow from 192.168.0.0/24 to any port 137,138 proto udp
sudo ufw --force enable
```

---

## Project structure

```
├── main.go                  # Entry point: HTTP routes, startup, shutdown
├── server/
│   ├── rooms.go             # Room map, participant management, TTL
│   ├── signaling.go         # WebSocket handler, message relay, input validation
│   ├── stats.go             # Anonymous SQLite stats
│   └── *_test.go            # 69 tests (go test ./server/... -race)
├── client/
│   ├── index.html           # Vite entry point (root of client/)
│   ├── vite.config.js       # Vite + vitest config
│   ├── public/              # Static assets (favicon, OG images, manifest)
│   ├── src/
│   │   ├── App.jsx          # Root: theme toggle, stats modal
│   │   └── components/
│   │       ├── IndexPage.jsx  # Landing page
│   │       └── Rooms.jsx      # Video room (WebRTC, dynamic grid)
│   │       └── Rooms.test.jsx # 14 component tests (npm test)
│   └── .env.example         # Template for TURN credentials
├── avsecure.service         # Reference systemd unit file
├── deploy-server.sh         # Build + deploy server binary to /opt/avsecure/
├── JOURNAL.md               # Change log
└── LICENSE                  # MIT
```

---

## Running the tests

```bash
# Go (69 tests)
go test ./server/... -race

# React (14 tests)
cd client && npm test
```

---

## License

MIT — see [LICENSE](LICENSE). Copyright 2026 Robin Macklepenny / Macklepenny Movement.
