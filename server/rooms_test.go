package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// wsPool spins up a minimal WS server and hands back server-side conns.
type wsPool struct {
	srv *httptest.Server
	ch  chan *websocket.Conn
}

func newWSPool() *wsPool {
	p := &wsPool{ch: make(chan *websocket.Conn, 10)}
	u := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := u.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		p.ch <- c
		// drain so the connection stays open
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
	}))
	return p
}

// Conn dials the pool server and returns the server-side *websocket.Conn.
func (p *wsPool) Conn(t *testing.T) *websocket.Conn {
	t.Helper()
	url := "ws://" + p.srv.Listener.Addr().String()
	_, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("wsPool.Conn dial: %v", err)
	}
	return <-p.ch
}

func (p *wsPool) Close() { p.srv.Close() }

// ── generateRoomID ──────────────────────────────────────────────────────────

func TestGenerateRoomID_Length(t *testing.T) {
	id := generateRoomID()
	if len(id) != 10 {
		t.Errorf("expected length 10, got %d (%q)", len(id), id)
	}
}

func TestGenerateRoomID_NoAmbiguousChars(t *testing.T) {
	ambiguous := "01OIl"
	for i := 0; i < 500; i++ {
		id := generateRoomID()
		for _, c := range ambiguous {
			if strings.ContainsRune(id, c) {
				t.Errorf("room ID %q contains ambiguous character %q", id, c)
			}
		}
	}
}

func TestGenerateRoomID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := generateRoomID()
		if seen[id] {
			t.Errorf("duplicate room ID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestGenerateRoomID_ValidCharsOnly(t *testing.T) {
	for i := 0; i < 200; i++ {
		id := generateRoomID()
		for _, c := range id {
			if !strings.ContainsRune(roomIDChars, c) {
				t.Errorf("room ID %q contains character %q not in allowed set", id, c)
			}
		}
	}
}

// ── RoomMap.CreateRoom ──────────────────────────────────────────────────────

func TestCreateRoom_ReturnsUniqueIDs(t *testing.T) {
	var rm RoomMap
	rm.Init()

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := rm.CreateRoom()
		if ids[id] {
			t.Errorf("duplicate room ID: %q", id)
		}
		ids[id] = true
	}
}

func TestCreateRoom_RoomExistsAfterCreate(t *testing.T) {
	var rm RoomMap
	rm.Init()

	id := rm.CreateRoom()
	if _, ok := rm.Get(id); !ok {
		t.Errorf("room %q not found after CreateRoom", id)
	}
}

// ── RoomMap.InsertIntoRoom ──────────────────────────────────────────────────

func TestInsertIntoRoom_Success(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn := pool.Conn(t)
	if err := rm.InsertIntoRoom(id, false, conn, "peer-1", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	participants, _ := rm.Get(id)
	if len(participants) != 1 {
		t.Errorf("expected 1 participant, got %d", len(participants))
	}
}

func TestInsertIntoRoom_RoomNotFound(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()

	conn := pool.Conn(t)
	err := rm.InsertIntoRoom("nonexistent", false, conn, "peer-1", "")
	if err == nil {
		t.Fatal("expected error for non-existent room, got nil")
	}
}

func TestInsertIntoRoom_RoomFull(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	for i := 0; i < maxParticipantsPerRoom; i++ {
		peerID := fmt.Sprintf("peer-%d", i)
		if err := rm.InsertIntoRoom(id, false, pool.Conn(t), peerID, ""); err != nil {
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	err := rm.InsertIntoRoom(id, false, pool.Conn(t), "overflow-peer", "")
	if err == nil {
		t.Fatal("expected error when room is full, got nil")
	}
}

func TestInsertIntoRoom_DuplicateConnection(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn := pool.Conn(t)
	if err := rm.InsertIntoRoom(id, false, conn, "dup-peer", ""); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := rm.InsertIntoRoom(id, false, conn, "dup-peer", ""); err == nil {
		t.Fatal("expected error on duplicate connection, got nil")
	}
}

// ── RoomMap.RemoveFromRoom ──────────────────────────────────────────────────

func TestRemoveFromRoom_RemovesParticipant(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	connA := pool.Conn(t)
	connB := pool.Conn(t)
	rm.InsertIntoRoom(id, false, connA, "peer-a", "")
	rm.InsertIntoRoom(id, false, connB, "peer-b", "")

	rm.RemoveFromRoom(id, connA)

	participants, ok := rm.Get(id)
	if !ok {
		t.Fatal("room should still exist after partial removal")
	}
	if len(participants) != 1 {
		t.Errorf("expected 1 participant, got %d", len(participants))
	}
	if participants[0].Conn != connB {
		t.Error("wrong participant remained")
	}
}

func TestRemoveFromRoom_DeletesEmptyRoom(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn := pool.Conn(t)
	rm.InsertIntoRoom(id, false, conn, "peer-1", "")
	rm.RemoveFromRoom(id, conn)

	if _, ok := rm.Get(id); ok {
		t.Error("empty room should be deleted after last participant leaves")
	}
}

func TestRemoveFromRoom_NonexistentRoom(t *testing.T) {
	var rm RoomMap
	rm.Init()
	// Should not panic on unknown room
	rm.RemoveFromRoom("ghost", nil)
}

// ── RoomMap.GetParticipantIDs ────────────────────────────────────────────────

func TestGetParticipantIDs_ReturnsAllIDs(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	rm.InsertIntoRoom(id, false, pool.Conn(t), "p1", "")
	rm.InsertIntoRoom(id, false, pool.Conn(t), "p2", "")
	rm.InsertIntoRoom(id, false, pool.Conn(t), "p3", "")

	ids := rm.GetParticipantIDs(id)
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d: %v", len(ids), ids)
	}
	seen := map[string]bool{}
	for _, pid := range ids {
		seen[pid] = true
	}
	for _, want := range []string{"p1", "p2", "p3"} {
		if !seen[want] {
			t.Errorf("missing peer ID %q in result", want)
		}
	}
}

func TestGetParticipantIDs_NilForMissingRoom(t *testing.T) {
	var rm RoomMap
	rm.Init()
	if rm.GetParticipantIDs("ghost") != nil {
		t.Error("expected nil for non-existent room")
	}
}

func TestGetParticipantIDs_ExcludesRemovedPeer(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	connA := pool.Conn(t)
	rm.InsertIntoRoom(id, false, connA, "peer-a", "")
	rm.InsertIntoRoom(id, false, pool.Conn(t), "peer-b", "")
	rm.RemoveFromRoom(id, connA)

	ids := rm.GetParticipantIDs(id)
	if len(ids) != 1 || ids[0] != "peer-b" {
		t.Errorf("expected [peer-b], got %v", ids)
	}
}

// ── RoomMap.Get ─────────────────────────────────────────────────────────────

func TestGet_NotFound(t *testing.T) {
	var rm RoomMap
	rm.Init()
	if _, ok := rm.Get("missing"); ok {
		t.Error("Get should return false for non-existent room")
	}
}

// ── RoomMap.GetParticipantInfo ────────────────────────────────────────────────

func TestGetParticipantInfo_ReturnsNicknamesWithIDs(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	rm.InsertIntoRoom(id, false, pool.Conn(t), "p1", "Tiger")
	rm.InsertIntoRoom(id, false, pool.Conn(t), "p2", "Bear")

	infos := rm.GetParticipantInfo(id)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}
	byID := map[string]string{}
	for _, info := range infos {
		byID[info.PeerID] = info.Nickname
	}
	if byID["p1"] != "Tiger" {
		t.Errorf("expected p1=Tiger, got %q", byID["p1"])
	}
	if byID["p2"] != "Bear" {
		t.Errorf("expected p2=Bear, got %q", byID["p2"])
	}
}

func TestGetParticipantInfo_NilForMissingRoom(t *testing.T) {
	var rm RoomMap
	rm.Init()
	if rm.GetParticipantInfo("ghost") != nil {
		t.Error("expected nil for non-existent room")
	}
}

// ── Concurrency smoke test ───────────────────────────────────────────────────

func TestRoomMap_ConcurrentCreateAndGet(t *testing.T) {
	var rm RoomMap
	rm.Init()

	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			id := rm.CreateRoom()
			rm.Get(id)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}
