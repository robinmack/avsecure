package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── isValidRoomID ────────────────────────────────────────────────────────────

func TestIsValidRoomID_ValidInputs(t *testing.T) {
	cases := []string{
		"abcd",           // minimum length
		"ABCD1234",
		"abcdefghij",     // 10 chars (generated length)
		"ab-cd_ef",       // hyphens and underscores allowed
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890",
		"a1B2c3D4e5",
	}
	for _, id := range cases {
		if !isValidRoomID(id) {
			t.Errorf("isValidRoomID(%q) = false, want true", id)
		}
	}
}

func TestIsValidRoomID_TooShort(t *testing.T) {
	cases := []string{"", "a", "ab", "abc"}
	for _, id := range cases {
		if isValidRoomID(id) {
			t.Errorf("isValidRoomID(%q) = true, want false (too short)", id)
		}
	}
}

func TestIsValidRoomID_TooLong(t *testing.T) {
	id := ""
	for i := 0; i < 65; i++ {
		id += "a"
	}
	if isValidRoomID(id) {
		t.Errorf("isValidRoomID(65-char string) = true, want false (too long)")
	}
}

func TestIsValidRoomID_InvalidChars(t *testing.T) {
	cases := []struct {
		id   string
		desc string
	}{
		{"abcd efgh", "space"},
		{"abcd\tefgh", "tab"},
		{"abcd/efgh", "forward slash"},
		{"abcd\\efgh", "backslash"},
		{"abcd.efgh", "period"},
		{"abcd@efgh", "at-sign"},
		{"<script>x</", "angle brackets"},
		{"abcd\nefgh", "newline"},
	}
	for _, c := range cases {
		if isValidRoomID(c.id) {
			t.Errorf("isValidRoomID(%q) = true, want false (%s)", c.id, c.desc)
		}
	}
}

func TestIsValidRoomID_ExactLengthBoundaries(t *testing.T) {
	// 4 chars: valid
	if !isValidRoomID("abcd") {
		t.Error("length-4 should be valid")
	}
	// 64 chars: valid
	id64 := ""
	for i := 0; i < 64; i++ {
		id64 += "a"
	}
	if !isValidRoomID(id64) {
		t.Errorf("length-64 should be valid")
	}
	// 65 chars: invalid
	if isValidRoomID(id64 + "a") {
		t.Error("length-65 should be invalid")
	}
}

// ── CreateRoomRequestHandler ─────────────────────────────────────────────────

func TestCreateRoomRequestHandler_ReturnsRoomID(t *testing.T) {
	AllRooms.Init()
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodGet, "/create", nil)
	rw  := httptest.NewRecorder()
	CreateRoomRequestHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}

	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RoomID == "" {
		t.Error("room_id is empty")
	}
	if len(resp.RoomID) != 10 {
		t.Errorf("room_id length = %d, want 10", len(resp.RoomID))
	}
}

func TestCreateRoomRequestHandler_RoomExistsAfterCreate(t *testing.T) {
	AllRooms.Init()
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodGet, "/create", nil)
	rw  := httptest.NewRecorder()
	CreateRoomRequestHandler(rw, req)

	var resp struct {
		RoomID string `json:"room_id"`
	}
	json.NewDecoder(rw.Body).Decode(&resp)

	if _, ok := AllRooms.Get(resp.RoomID); !ok {
		t.Errorf("room %q not found in AllRooms after creation", resp.RoomID)
	}
}

func TestCreateRoomRequestHandler_OPTIONS_Returns204(t *testing.T) {
	AllRooms.Init()

	req := httptest.NewRequest(http.MethodOptions, "/create", nil)
	rw  := httptest.NewRecorder()
	CreateRoomRequestHandler(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rw.Code)
	}
}

func TestCreateRoomRequestHandler_SetsCORSHeader(t *testing.T) {
	AllRooms.Init()
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodGet, "/create", nil)
	rw  := httptest.NewRecorder()
	CreateRoomRequestHandler(rw, req)

	if got := rw.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("CORS origin = %q, want %q", got, allowedOrigin)
	}
}

func TestCreateRoomRequestHandler_IncrementsRoomsCreated(t *testing.T) {
	AllRooms.Init()
	defer freshDB(t)()

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/create", nil)
		CreateRoomRequestHandler(httptest.NewRecorder(), req)
	}

	if got := queryInt(`SELECT value FROM counters WHERE key='rooms_created'`); got != 3 {
		t.Errorf("rooms_created = %d, want 3", got)
	}
}

func TestCreateRoomRequestHandler_UniqueIDsOnMultipleCalls(t *testing.T) {
	AllRooms.Init()
	defer freshDB(t)()

	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/create", nil)
		rw  := httptest.NewRecorder()
		CreateRoomRequestHandler(rw, req)

		var resp struct {
			RoomID string `json:"room_id"`
		}
		json.NewDecoder(rw.Body).Decode(&resp)

		if ids[resp.RoomID] {
			t.Errorf("duplicate room_id returned: %q", resp.RoomID)
		}
		ids[resp.RoomID] = true
	}
}

// ── JoinRoomRequestHandler — validation layer ─────────────────────────────────
// (Full WebSocket join requires a live connection; these cover the HTTP-level guards.)

func TestJoinRoomRequestHandler_MissingRoomID_Returns400(t *testing.T) {
	AllRooms.Init()

	req := httptest.NewRequest(http.MethodGet, "/join", nil)
	rw  := httptest.NewRecorder()
	JoinRoomRequestHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestJoinRoomRequestHandler_InvalidRoomIDChars_Returns400(t *testing.T) {
	AllRooms.Init()

	req := httptest.NewRequest(http.MethodGet, "/join?roomID=../../etc/passwd", nil)
	rw  := httptest.NewRecorder()
	JoinRoomRequestHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestJoinRoomRequestHandler_ShortRoomID_Returns400(t *testing.T) {
	AllRooms.Init()

	req := httptest.NewRequest(http.MethodGet, "/join?roomID=ab", nil)
	rw  := httptest.NewRecorder()
	JoinRoomRequestHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

