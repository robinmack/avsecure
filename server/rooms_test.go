package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestRemoveFromRoom_PersistsWhenEmpty(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn := pool.Conn(t)
	rm.InsertIntoRoom(id, false, conn, "peer-1", "")
	rm.RemoveFromRoom(id, conn)

	if _, ok := rm.Get(id); !ok {
		t.Error("room should persist after last participant leaves (persistent rooms)")
	}
}

// ── Room TTL / persistence ───────────────────────────────────────────────────

func TestRoom_CanRejoinAfterLeaving(t *testing.T) {
	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn1 := pool.Conn(t)
	if err := rm.InsertIntoRoom(id, false, conn1, "peer-1", "Alice"); err != nil {
		t.Fatalf("first join: %v", err)
	}
	rm.RemoveFromRoom(id, conn1)

	conn2 := pool.Conn(t)
	if err := rm.InsertIntoRoom(id, false, conn2, "peer-2", "Bob"); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	ids := rm.GetParticipantIDs(id)
	if len(ids) != 1 || ids[0] != "peer-2" {
		t.Errorf("expected [peer-2], got %v", ids)
	}
}

func TestInsertIntoRoom_RejectsExpiredRoom(t *testing.T) {
	old := roomTTL
	roomTTL = 50 * time.Millisecond
	defer func() { roomTTL = old }()

	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	time.Sleep(100 * time.Millisecond) // well past 50ms TTL

	err := rm.InsertIntoRoom(id, false, pool.Conn(t), "late-peer", "")
	if err == nil {
		t.Error("expected error when joining an expired empty room")
	}
}

func TestSweepExpired_RemovesExpiredEmptyRoom(t *testing.T) {
	old := roomTTL
	roomTTL = 50 * time.Millisecond
	defer func() { roomTTL = old }()

	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	conn := pool.Conn(t)
	if err := rm.InsertIntoRoom(id, false, conn, "peer-1", ""); err != nil {
		t.Fatalf("InsertIntoRoom: %v", err)
	}
	rm.RemoveFromRoom(id, conn) // resets TTL to now+50ms

	time.Sleep(100 * time.Millisecond) // well past 50ms
	rm.SweepExpired()

	if _, ok := rm.Get(id); ok {
		t.Error("expired empty room should be removed by SweepExpired")
	}
}

func TestSweepExpired_KeepsActiveRoom(t *testing.T) {
	old := roomTTL
	roomTTL = 50 * time.Millisecond
	defer func() { roomTTL = old }()

	pool := newWSPool()
	defer pool.Close()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	if err := rm.InsertIntoRoom(id, false, pool.Conn(t), "peer-1", ""); err != nil {
		t.Fatalf("InsertIntoRoom: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // past TTL, but room has a participant
	rm.SweepExpired()

	if _, ok := rm.Get(id); !ok {
		t.Error("room with active participants must not be swept regardless of TTL")
	}
}

func TestSweepExpired_KeepsNonExpiredRoom(t *testing.T) {
	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	rm.SweepExpired()

	if _, ok := rm.Get(id); !ok {
		t.Error("non-expired room should survive a sweep")
	}
}

func TestTouch_ExtendsRoomTTL(t *testing.T) {
	old := roomTTL
	roomTTL = 50 * time.Millisecond
	defer func() { roomTTL = old }()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	time.Sleep(30 * time.Millisecond) // near expiry
	rm.Touch(id)                      // reset TTL to now+50ms

	time.Sleep(30 * time.Millisecond) // past original expiry, within extended window
	rm.SweepExpired()

	if _, ok := rm.Get(id); !ok {
		t.Error("Touch should extend the room TTL")
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

// ── SweepExpired returns IDs ──────────────────────────────────────────────────

func TestSweepExpired_ReturnsSweptRoomIDs(t *testing.T) {
	old := roomTTL
	roomTTL = 50 * time.Millisecond
	defer func() { roomTTL = old }()

	var rm RoomMap
	rm.Init()
	id := rm.CreateRoom()

	time.Sleep(100 * time.Millisecond)
	swept := rm.SweepExpired()

	if len(swept) != 1 || swept[0] != id {
		t.Errorf("expected [%q], got %v", id, swept)
	}
}

func TestSweepExpired_ReturnsNilWhenNothingExpired(t *testing.T) {
	var rm RoomMap
	rm.Init()
	rm.CreateRoom()

	swept := rm.SweepExpired()
	if len(swept) != 0 {
		t.Errorf("expected empty, got %v", swept)
	}
}

// ── RoomMap.Restore ───────────────────────────────────────────────────────────

func TestRestore_RecreatesRooms(t *testing.T) {
	var rm RoomMap
	rm.Init()

	future := time.Now().Add(time.Hour)
	rm.Restore(map[string]time.Time{"room-A": future, "room-B": future})

	for _, id := range []string{"room-A", "room-B"} {
		if _, ok := rm.Get(id); !ok {
			t.Errorf("room %q should exist after Restore", id)
		}
	}
}

func TestRestore_IgnoresExistingRooms(t *testing.T) {
	var rm RoomMap
	rm.Init()
	existing := rm.CreateRoom()

	// Restore must not overwrite a room that's already live.
	rm.Restore(map[string]time.Time{existing: time.Now().Add(time.Hour)})

	if _, ok := rm.Get(existing); !ok {
		t.Error("existing room should survive Restore")
	}
}

// ── RoomMap.Snapshot ──────────────────────────────────────────────────────────

func TestSnapshot_ContainsAllRooms(t *testing.T) {
	var rm RoomMap
	rm.Init()
	id1 := rm.CreateRoom()
	id2 := rm.CreateRoom()

	snap := rm.Snapshot()
	for _, id := range []string{id1, id2} {
		if _, ok := snap[id]; !ok {
			t.Errorf("snapshot missing %q", id)
		}
	}
}

func TestSnapshot_EmptyWhenNoRooms(t *testing.T) {
	var rm RoomMap
	rm.Init()
	if n := len(rm.Snapshot()); n != 0 {
		t.Errorf("expected empty snapshot, got %d entries", n)
	}
}

// ── RoomMap.BroadcastToAll ────────────────────────────────────────────────────

func TestBroadcastToAll_NoopOnEmptyRoom(t *testing.T) {
	var rm RoomMap
	rm.Init()
	rm.CreateRoom()
	rm.BroadcastToAll(map[string]interface{}{"type": "restart", "delay": 3000})
}

func TestBroadcastToAll_ReachesAllParticipants(t *testing.T) {
	u := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConnCh := make(chan *websocket.Conn, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := u.Upgrade(w, r, nil)
		serverConnCh <- c
		for { if _, _, err := c.ReadMessage(); err != nil { break } }
	}))
	defer srv.Close()

	var rm RoomMap
	rm.Init()
	roomID := rm.CreateRoom()

	type pair struct{ msgs chan map[string]interface{} }
	pairs := make([]pair, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		clientConn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		serverConn := <-serverConnCh
		rm.InsertIntoRoom(roomID, false, serverConn, fmt.Sprintf("p%d", i), "")

		ch := make(chan map[string]interface{}, 2)
		pairs[i] = pair{msgs: ch}
		wg.Add(1)
		go func(c *websocket.Conn, ch chan map[string]interface{}) {
			defer wg.Done()
			var m map[string]interface{}
			if err := c.ReadJSON(&m); err == nil {
				ch <- m
			}
		}(clientConn, ch)
	}

	rm.BroadcastToAll(map[string]interface{}{"type": "restart", "delay": 3000})

	for i, p := range pairs {
		select {
		case msg := <-p.msgs:
			if msg["type"] != "restart" {
				t.Errorf("pair %d: got type %q, want 'restart'", i, msg["type"])
			}
		case <-time.After(time.Second):
			t.Errorf("pair %d: timed out waiting for restart", i)
		}
	}
}

// ── RoomMap.AtCapacity / room count cap ──────────────────────────────────────

func TestAtCapacity_FalseWhenEmpty(t *testing.T) {
	var rm RoomMap
	rm.Init()
	if rm.AtCapacity() {
		t.Error("empty RoomMap should not be at capacity")
	}
}

func TestAtCapacity_TrueAfterMaxRooms(t *testing.T) {
	old := maxRooms
	maxRooms = 3
	defer func() { maxRooms = old }()

	var rm RoomMap
	rm.Init()
	for i := 0; i < 3; i++ {
		rm.CreateRoom()
	}
	if !rm.AtCapacity() {
		t.Errorf("should be at capacity after %d rooms", maxRooms)
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
