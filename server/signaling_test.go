package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// ── sanitizeNickname ──────────────────────────────────────────────────────────

func TestSanitizeNickname_TruncatesLongNickname(t *testing.T) {
	long := strings.Repeat("a", maxNicknameLen+10)
	got := sanitizeNickname(long)
	if len([]rune(got)) > maxNicknameLen {
		t.Errorf("expected max %d runes, got %d", maxNicknameLen, len([]rune(got)))
	}
}

func TestSanitizeNickname_TrimsWhitespace(t *testing.T) {
	if got := sanitizeNickname("  Alice  "); got != "Alice" {
		t.Errorf("want %q, got %q", "Alice", got)
	}
}

func TestSanitizeNickname_HandlesUnicode(t *testing.T) {
	// 5 Japanese runes = 15 bytes — must count runes, not bytes
	name := "こんにちは"
	got := sanitizeNickname(name)
	if len([]rune(got)) != 5 {
		t.Errorf("want 5 runes, got %d", len([]rune(got)))
	}
}

func TestSanitizeNickname_TruncatesLongUnicode(t *testing.T) {
	// 30 emoji, each multi-byte — truncation must still work correctly
	long := strings.Repeat("🦊", maxNicknameLen+6)
	got := sanitizeNickname(long)
	if len([]rune(got)) > maxNicknameLen {
		t.Errorf("expected max %d runes, got %d", maxNicknameLen, len([]rune(got)))
	}
}

// ── isRelayableType ───────────────────────────────────────────────────────────

func TestIsRelayableType_AllowsSignalingMessages(t *testing.T) {
	for _, typ := range []string{"offer", "answer", "iceCandidate"} {
		if !isRelayableType(typ) {
			t.Errorf("expected %q to be relayable", typ)
		}
	}
}

func TestIsRelayableType_BlocksNonSignalingTypes(t *testing.T) {
	blocked := []string{
		"ping", "pong", "join", "roster", "leave", "error", "",
		"DROP TABLE rooms", "<script>alert(1)</script>",
	}
	for _, typ := range blocked {
		if isRelayableType(typ) {
			t.Errorf("expected %q to be blocked from relay", typ)
		}
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

