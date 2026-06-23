package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// freshDB calls InitStats with a unique temp-dir path and returns a cleanup func.
func freshDB(t *testing.T) func() {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	if err := InitStats(path); err != nil {
		t.Fatalf("InitStats: %v", err)
	}
	return func() {
		if statsDB != nil {
			statsDB.Close()
			statsDB = nil
		}
	}
}

// ── InitStats ───────────────────────────────────────────────────────────────

func TestInitStats_SeedsCounters(t *testing.T) {
	defer freshDB(t)()

	visits := queryInt(`SELECT value FROM counters WHERE key='visits'`)
	rooms  := queryInt(`SELECT value FROM counters WHERE key='rooms_created'`)

	if visits != 0 {
		t.Errorf("initial visits = %d, want 0", visits)
	}
	if rooms != 0 {
		t.Errorf("initial rooms_created = %d, want 0", rooms)
	}
}

func TestInitStats_CreatesTablesIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 3; i++ {
		if err := InitStats(path); err != nil {
			t.Fatalf("InitStats call %d: %v", i+1, err)
		}
	}
	if statsDB != nil {
		statsDB.Close()
		statsDB = nil
	}
}

// ── RecordVisit / RecordRoom ─────────────────────────────────────────────────

func TestRecordVisit_IncrementsCounter(t *testing.T) {
	defer freshDB(t)()

	RecordVisit()
	RecordVisit()
	RecordVisit()

	if got := queryInt(`SELECT value FROM counters WHERE key='visits'`); got != 3 {
		t.Errorf("visits = %d, want 3", got)
	}
}

func TestRecordRoom_IncrementsCounter(t *testing.T) {
	defer freshDB(t)()

	RecordRoom()
	RecordRoom()

	if got := queryInt(`SELECT value FROM counters WHERE key='rooms_created'`); got != 2 {
		t.Errorf("rooms_created = %d, want 2", got)
	}
}

func TestRecordVisit_NoPanicWhenDBNil(t *testing.T) {
	statsDB = nil
	RecordVisit() // must not panic
}

// ── RecordChat ───────────────────────────────────────────────────────────────

func TestRecordChat_InsertsSession(t *testing.T) {
	defer freshDB(t)()

	RecordChat(120)

	if got := queryInt(`SELECT COUNT(*) FROM chat_sessions`); got != 1 {
		t.Errorf("chat_sessions count = %d, want 1", got)
	}
	if got := queryInt(`SELECT duration_seconds FROM chat_sessions`); got != 120 {
		t.Errorf("duration_seconds = %d, want 120", got)
	}
}

func TestRecordChat_IgnoresZeroDuration(t *testing.T) {
	defer freshDB(t)()

	RecordChat(0)
	RecordChat(-5)

	if got := queryInt(`SELECT COUNT(*) FROM chat_sessions`); got != 0 {
		t.Errorf("expected 0 rows for invalid durations, got %d", got)
	}
}

func TestRecordChat_MultipleSessionsAccumulate(t *testing.T) {
	defer freshDB(t)()

	RecordChat(60)
	RecordChat(120)
	RecordChat(180)

	count := queryInt(`SELECT COUNT(*) FROM chat_sessions`)
	total := queryInt(`SELECT SUM(duration_seconds) FROM chat_sessions`)

	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if total != 360 {
		t.Errorf("total duration = %d, want 360", total)
	}
}

// ── VisitHandler ─────────────────────────────────────────────────────────────

func TestVisitHandler_POST_Returns204(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodPost, "/stats/visit", nil)
	rw  := httptest.NewRecorder()
	VisitHandler(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rw.Code)
	}
}

func TestVisitHandler_OPTIONS_Returns204(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodOptions, "/stats/visit", nil)
	rw  := httptest.NewRecorder()
	VisitHandler(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rw.Code)
	}
}

func TestVisitHandler_SetsCORSHeader(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodPost, "/stats/visit", nil)
	rw  := httptest.NewRecorder()
	VisitHandler(rw, req)

	got := rw.Header().Get("Access-Control-Allow-Origin")
	if got != allowedOrigin {
		t.Errorf("CORS origin = %q, want %q", got, allowedOrigin)
	}
}

func TestVisitHandler_IncreasesVisitCount(t *testing.T) {
	defer freshDB(t)()

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/stats/visit", nil)
		VisitHandler(httptest.NewRecorder(), req)
	}

	if got := queryInt(`SELECT value FROM counters WHERE key='visits'`); got != 5 {
		t.Errorf("visits = %d, want 5", got)
	}
}

// ── ChatHandler ──────────────────────────────────────────────────────────────

func TestChatHandler_ValidBody_Returns204(t *testing.T) {
	defer freshDB(t)()

	body := strings.NewReader(`{"duration_seconds": 90}`)
	req  := httptest.NewRequest(http.MethodPost, "/stats/chat", body)
	req.Header.Set("Content-Type", "application/json")
	rw   := httptest.NewRecorder()
	ChatHandler(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rw.Code)
	}
}

func TestChatHandler_ZeroDuration_Returns400(t *testing.T) {
	defer freshDB(t)()

	body := strings.NewReader(`{"duration_seconds": 0}`)
	req  := httptest.NewRequest(http.MethodPost, "/stats/chat", body)
	req.Header.Set("Content-Type", "application/json")
	rw   := httptest.NewRecorder()
	ChatHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestChatHandler_MalformedJSON_Returns400(t *testing.T) {
	defer freshDB(t)()

	body := strings.NewReader(`not json`)
	req  := httptest.NewRequest(http.MethodPost, "/stats/chat", body)
	rw   := httptest.NewRecorder()
	ChatHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestChatHandler_OPTIONS_Returns204(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodOptions, "/stats/chat", nil)
	rw  := httptest.NewRecorder()
	ChatHandler(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rw.Code)
	}
}

// ── PublicStatsHandler ───────────────────────────────────────────────────────

func TestPublicStatsHandler_Returns200WithJSON(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodGet, "/stats/public", nil)
	rw  := httptest.NewRecorder()
	PublicStatsHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}

	var s PublicStats
	if err := json.NewDecoder(rw.Body).Decode(&s); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestPublicStatsHandler_ReflectsRecordedData(t *testing.T) {
	defer freshDB(t)()

	RecordVisit()
	RecordVisit()
	RecordRoom()
	RecordChat(300)
	RecordChat(600)

	req := httptest.NewRequest(http.MethodGet, "/stats/public", nil)
	rw  := httptest.NewRecorder()
	PublicStatsHandler(rw, req)

	var s PublicStats
	json.NewDecoder(rw.Body).Decode(&s)

	if s.Visits != 2 {
		t.Errorf("Visits = %d, want 2", s.Visits)
	}
	if s.RoomsCreated != 1 {
		t.Errorf("RoomsCreated = %d, want 1", s.RoomsCreated)
	}
	if s.ChatsTotal != 2 {
		t.Errorf("ChatsTotal = %d, want 2", s.ChatsTotal)
	}
	// (300 + 600) / 2 = 450
	if s.AvgDurAll != 450 {
		t.Errorf("AvgDurAll = %v, want 450", s.AvgDurAll)
	}
	// (300 + 600) / 60 = 15
	if s.TotalMinutes != 15 {
		t.Errorf("TotalMinutes = %d, want 15", s.TotalMinutes)
	}
}

func TestPublicStatsHandler_UnavailableWhenDBNil(t *testing.T) {
	statsDB = nil

	req := httptest.NewRequest(http.MethodGet, "/stats/public", nil)
	rw  := httptest.NewRecorder()
	PublicStatsHandler(rw, req)

	if rw.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rw.Code)
	}
}

func TestPublicStatsHandler_SetsCORSHeader(t *testing.T) {
	defer freshDB(t)()

	req := httptest.NewRequest(http.MethodGet, "/stats/public", nil)
	rw  := httptest.NewRecorder()
	PublicStatsHandler(rw, req)

	if got := rw.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("CORS origin = %q, want %q", got, allowedOrigin)
	}
}
