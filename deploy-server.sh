#!/bin/bash
set -e
cd "$(dirname "$0")"

go build -o go-react-webrtc-linux .
sudo cp go-react-webrtc-linux /opt/avsecure/go-react-webrtc
sudo chown avsecure:avsecure /opt/avsecure/go-react-webrtc
sudo systemctl restart avsecure
echo "Server deployed at $(date)"
