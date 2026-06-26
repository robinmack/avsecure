package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const maxParticipantsPerRoom = 8

// maxRooms is a var (not const) so tests can override it.
var maxRooms = 1000

// roomTTL is a var (not const) so tests can override it.
var roomTTL = 4 * time.Hour

type Participant struct {
	Host     bool
	Conn     *websocket.Conn
	Mutex    sync.Mutex
	PeerID   string
	Nickname string
}

// ParticipantInfo is the wire-format entry sent in roster/join messages.
type ParticipantInfo struct {
	PeerID   string `json:"peerId"`
	Nickname string `json:"nickname"`
}

type RoomMap struct {
	mu        sync.RWMutex
	Map       map[string][]*Participant
	expiresAt map[string]time.Time
}

func (r *RoomMap) Init() {
	r.Map = make(map[string][]*Participant)
	r.expiresAt = make(map[string]time.Time)
}

// AtCapacity returns true when the room count has reached maxRooms.
func (r *RoomMap) AtCapacity() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Map) >= maxRooms
}

func (r *RoomMap) Get(roomID string) ([]*Participant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	participants, exists := r.Map[roomID]
	return participants, exists
}

func (r *RoomMap) CreateRoom() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		id := generateRoomID()
		if _, exists := r.Map[id]; !exists {
			r.Map[id] = []*Participant{}
			r.expiresAt[id] = time.Now().Add(roomTTL)
			return id
		}
	}
}

func (r *RoomMap) InsertIntoRoom(roomID string, host bool, conn *websocket.Conn, peerID, nickname string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	participants, exists := r.Map[roomID]
	if !exists {
		return fmt.Errorf("room %s does not exist", roomID)
	}
	// Only check expiry when the room is empty — an active room is never expired.
	if len(participants) == 0 {
		if exp, ok := r.expiresAt[roomID]; ok && time.Now().After(exp) {
			return fmt.Errorf("room %s has expired", roomID)
		}
	}
	if len(participants) >= maxParticipantsPerRoom {
		return fmt.Errorf("room %s is full", roomID)
	}
	for _, p := range participants {
		if p.Conn == conn {
			return fmt.Errorf("already in room %s", roomID)
		}
	}
	r.Map[roomID] = append(r.Map[roomID], &Participant{Host: host, Conn: conn, PeerID: peerID, Nickname: nickname})
	r.expiresAt[roomID] = time.Now().Add(roomTTL) // activity extends TTL
	return nil
}

// RemoveFromRoom removes a connection. The room persists when empty; SweepExpired handles cleanup.
func (r *RoomMap) RemoveFromRoom(roomID string, conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	participants, exists := r.Map[roomID]
	if !exists {
		return
	}
	updated := participants[:0]
	for _, p := range participants {
		if p.Conn != conn {
			updated = append(updated, p)
		}
	}
	r.Map[roomID] = updated
	if len(updated) == 0 {
		// Room is now empty — start the inactivity countdown from now.
		r.expiresAt[roomID] = time.Now().Add(roomTTL)
	}
}

// Touch resets the expiry timer for a room, called on any signaling activity.
func (r *RoomMap) Touch(roomID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.Map[roomID]; exists {
		r.expiresAt[roomID] = time.Now().Add(roomTTL)
	}
}

// SweepExpired deletes rooms that are both empty and past their TTL.
// Returns the IDs of swept rooms so callers can clean them from persistent storage.
func (r *RoomMap) SweepExpired() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var swept []string
	now := time.Now()
	for id := range r.expiresAt {
		if now.After(r.expiresAt[id]) && len(r.Map[id]) == 0 {
			delete(r.Map, id)
			delete(r.expiresAt, id)
			swept = append(swept, id)
		}
	}
	return swept
}

// Restore loads room IDs and TTLs from a prior run, skipping any that already exist.
func (r *RoomMap) Restore(rooms map[string]time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, exp := range rooms {
		if _, exists := r.Map[id]; !exists {
			r.Map[id] = []*Participant{}
			r.expiresAt[id] = exp
		}
	}
}

// Snapshot returns a copy of all current room IDs with their expiry times.
func (r *RoomMap) Snapshot() map[string]time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]time.Time, len(r.expiresAt))
	for id, exp := range r.expiresAt {
		out[id] = exp
	}
	return out
}

// BroadcastToAll sends msg to every connected participant across all rooms.
// Used for server-wide notifications such as graceful-restart warnings.
func (r *RoomMap) BroadcastToAll(msg map[string]interface{}) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, participants := range r.Map {
		for _, p := range participants {
			p.Mutex.Lock()
			p.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = p.Conn.WriteJSON(msg)
			p.Mutex.Unlock()
		}
	}
}

func (r *RoomMap) DeleteRoom(roomID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if participants, exists := r.Map[roomID]; exists {
		for _, p := range participants {
			p.Conn.Close()
		}
		delete(r.Map, roomID)
		delete(r.expiresAt, roomID)
	}
}

// GetParticipantIDs returns the PeerID of every current participant in roomID.
// Returns nil if the room does not exist.
func (r *RoomMap) GetParticipantIDs(roomID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	participants, exists := r.Map[roomID]
	if !exists {
		return nil
	}
	ids := make([]string, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.PeerID)
	}
	return ids
}

// GetParticipantInfo returns PeerID+Nickname for every current participant.
// Returns nil if the room does not exist.
func (r *RoomMap) GetParticipantInfo(roomID string) []ParticipantInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	participants, exists := r.Map[roomID]
	if !exists {
		return nil
	}
	infos := make([]ParticipantInfo, 0, len(participants))
	for _, p := range participants {
		infos = append(infos, ParticipantInfo{PeerID: p.PeerID, Nickname: p.Nickname})
	}
	return infos
}

// Excludes ambiguous characters: 0/O, 1/l/I to prevent transcription errors
const roomIDChars = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

func generateRoomID() string {
	b := make([]byte, 10)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(roomIDChars))))
		b[i] = roomIDChars[n.Int64()]
	}
	return string(b)
}
