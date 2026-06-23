package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/gorilla/websocket"
)

const maxParticipantsPerRoom = 8

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
	mu  sync.RWMutex
	Map map[string][]*Participant
}

func (r *RoomMap) Init() {
	r.Map = make(map[string][]*Participant)
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
	if len(participants) >= maxParticipantsPerRoom {
		return fmt.Errorf("room %s is full", roomID)
	}
	for _, p := range participants {
		if p.Conn == conn {
			return fmt.Errorf("already in room %s", roomID)
		}
	}
	r.Map[roomID] = append(r.Map[roomID], &Participant{Host: host, Conn: conn, PeerID: peerID, Nickname: nickname})
	return nil
}

// RemoveFromRoom removes a connection and cleans up empty rooms.
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
	if len(updated) == 0 {
		delete(r.Map, roomID)
	} else {
		r.Map[roomID] = updated
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
