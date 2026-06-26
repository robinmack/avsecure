package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxNicknameLen  = 24
	maxMessageBytes = 65536 // 64 KB — generous for SDP, blocks message bombs
)

// sanitizeNickname trims whitespace and enforces the rune-count limit.
func sanitizeNickname(s string) string {
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > maxNicknameLen {
		s = string(runes[:maxNicknameLen])
	}
	return s
}

// isRelayableType returns true only for message types the server should forward
// between peers. Anything else (ping, join, unknown) is dropped at the relay layer.
func isRelayableType(t string) bool {
	switch t {
	case "offer", "answer", "iceCandidate":
		return true
	}
	return false
}

var (
	AllRooms  RoomMap
	Broadcast = make(chan BroadcastMsg, 256)
)

const allowedOrigin = "https://avsecure.vip"

func CreateRoomRequestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if AllRooms.AtCapacity() {
		http.Error(w, "server at capacity", http.StatusServiceUnavailable)
		return
	}

	roomID := AllRooms.CreateRoom()
	go PersistRoom(roomID, time.Now().Add(roomTTL))
	RecordRoom()

	type resp struct {
		RoomID string `json:"room_id"`
	}

	log.Printf("Room created: %s", roomID)
	if err := json.NewEncoder(w).Encode(resp{RoomID: roomID}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == allowedOrigin
	},
}

type BroadcastMsg struct {
	Message map[string]interface{}
	RoomID  string
	Client  *websocket.Conn
	From    string // sender's PeerID
	To      string // recipient's PeerID; empty = broadcast to all except sender
}

// Broadcaster runs as a single goroutine started in main.
func Broadcaster() {
	for msg := range Broadcast {
		clients, exists := AllRooms.Get(msg.RoomID)
		if !exists {
			log.Printf("Room %s not found during broadcast", msg.RoomID)
			continue
		}

		if msg.To != "" {
			// Targeted: deliver only to the named recipient.
			for _, client := range clients {
				if client.PeerID != msg.To {
					continue
				}
				client.Mutex.Lock()
				client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := client.Conn.WriteJSON(msg.Message)
				client.Mutex.Unlock()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("WebSocket write error to %s: %v", msg.To, err)
					}
					client.Conn.Close()
				}
				break
			}
		} else {
			// Broadcast: send to everyone except the sender connection.
			for _, client := range clients {
				if client.Conn == msg.Client {
					continue
				}
				client.Mutex.Lock()
				client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := client.Conn.WriteJSON(msg.Message)
				client.Mutex.Unlock()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("WebSocket write error: %v", err)
					}
					client.Conn.Close()
				}
			}
		}
	}
}

func JoinRoomRequestHandler(w http.ResponseWriter, r *http.Request) {
	roomIDs, ok := r.URL.Query()["roomID"]
	if !ok || len(roomIDs[0]) < 1 {
		http.Error(w, "roomID missing", http.StatusBadRequest)
		return
	}
	roomID := roomIDs[0]

	if !isValidRoomID(roomID) {
		http.Error(w, "invalid roomID", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer ws.Close()
	ws.SetReadLimit(maxMessageBytes)

	// Read the initial join handshake to learn this peer's self-assigned ID.
	var joinMsg map[string]interface{}
	if err := ws.ReadJSON(&joinMsg); err != nil || joinMsg["type"] != "join" {
		log.Printf("Room %s: expected join handshake", roomID)
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "expected join message"))
		return
	}
	peerID, _ := joinMsg["peerId"].(string)
	if peerID == "" {
		log.Printf("Room %s: join message missing peerId", roomID)
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "missing peerId"))
		return
	}
	nicknameRaw, _ := joinMsg["nickname"].(string)
	nickname := sanitizeNickname(nicknameRaw)
	if nickname == "" {
		nickname = "Anonymous"
	}

	// Snapshot existing participants before inserting the newcomer.
	existingInfo := AllRooms.GetParticipantInfo(roomID)

	if err := AllRooms.InsertIntoRoom(roomID, false, ws, peerID, nickname); err != nil {
		log.Printf("InsertIntoRoom error: %v", err)
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()))
		return
	}
	defer AllRooms.RemoveFromRoom(roomID, ws)

	log.Printf("Peer %s (%s) joined room %s (%d existing peers)", peerID, nickname, roomID, len(existingInfo))

	// Send the roster to the newcomer (may be empty for the first participant).
	if err := ws.WriteJSON(map[string]interface{}{
		"type":  "roster",
		"peers": existingInfo,
	}); err != nil {
		log.Printf("Failed to send roster to %s: %v", peerID, err)
		return
	}

	// Notify existing participants that a new peer has joined.
	Broadcast <- BroadcastMsg{
		Message: map[string]interface{}{"type": "join", "peerId": peerID, "nickname": nickname},
		RoomID:  roomID,
		Client:  ws,
	}

	// Relay loop: forward messages to their intended recipients.
	for {
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error from %s: %v", peerID, err)
			}
			break
		}

		// Every message from an active client keeps the room alive.
		AllRooms.Touch(roomID)

		msgType, _ := msg["type"].(string)

		// Ping/pong: client heartbeat to maintain room TTL; not relayed.
		if msgType == "ping" {
			ws.WriteJSON(map[string]interface{}{"type": "pong"})
			continue
		}

		// Only relay the three signaling message types; drop everything else.
		if !isRelayableType(msgType) {
			continue
		}

		to, _ := msg["to"].(string)
		from, _ := msg["from"].(string)
		Broadcast <- BroadcastMsg{
			Message: msg,
			RoomID:  roomID,
			Client:  ws,
			To:      to,
			From:    from,
		}
	}

	// Announce departure to remaining participants before defer fires.
	Broadcast <- BroadcastMsg{
		Message: map[string]interface{}{"type": "leave", "peerId": peerID},
		RoomID:  roomID,
		Client:  ws,
	}
}

func isValidRoomID(id string) bool {
	if len(id) < 4 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
