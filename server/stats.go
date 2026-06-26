package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	_ "modernc.org/sqlite"
)

var statsDB *sql.DB

func InitStats(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1) // SQLite writer serialisation

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS counters (
			key   TEXT PRIMARY KEY,
			value INTEGER NOT NULL DEFAULT 0
		);
		INSERT OR IGNORE INTO counters (key, value) VALUES
			('visits', 0),
			('rooms_created', 0);

		CREATE TABLE IF NOT EXISTS chat_sessions (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			duration_seconds INTEGER NOT NULL,
			created_at       DATETIME DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS rooms (
			id         TEXT PRIMARY KEY,
			expires_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}
	statsDB = db
	log.Println("Stats DB initialised:", dbPath)
	return nil
}

// ── Room persistence ──────────────────────────────────────────────────────────

// PersistRoom upserts a room's expiry into the rooms table.
// Failures are logged but never returned — persistence is best-effort.
func PersistRoom(id string, expiresAt time.Time) {
	if statsDB == nil {
		return
	}
	if _, err := statsDB.Exec(
		`INSERT OR REPLACE INTO rooms (id, expires_at) VALUES (?, ?)`,
		id, expiresAt.Unix(),
	); err != nil {
		log.Printf("persist room %s: %v", id, err)
	}
}

// RemovePersistedRooms deletes the given room IDs from the rooms table.
func RemovePersistedRooms(ids []string) {
	if statsDB == nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		if _, err := statsDB.Exec(`DELETE FROM rooms WHERE id = ?`, id); err != nil {
			log.Printf("remove persisted room %s: %v", id, err)
		}
	}
}

// LoadPersistedRooms returns all non-expired rooms from the rooms table.
func LoadPersistedRooms() (map[string]time.Time, error) {
	if statsDB == nil {
		return nil, nil
	}
	rows, err := statsDB.Query(
		`SELECT id, expires_at FROM rooms WHERE expires_at > ?`, time.Now().Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]time.Time)
	for rows.Next() {
		var id string
		var expUnix int64
		if err := rows.Scan(&id, &expUnix); err != nil {
			continue
		}
		result[id] = time.Unix(expUnix, 0)
	}
	return result, nil
}

// SyncRoomsToDB batch-upserts a room snapshot — called periodically so TTLs
// stay accurate across restarts even without per-Touch writes.
func SyncRoomsToDB(rooms map[string]time.Time) {
	if statsDB == nil || len(rooms) == 0 {
		return
	}
	for id, exp := range rooms {
		PersistRoom(id, exp)
	}
	// Clean up rows for rooms that no longer exist in memory.
	if _, err := statsDB.Exec(
		`DELETE FROM rooms WHERE expires_at <= ?`, time.Now().Unix(),
	); err != nil {
		log.Printf("sync rooms cleanup: %v", err)
	}
}

// ── Counters ──────────────────────────────────────────────────────────────────

func incrementCounter(key string) {
	if statsDB == nil {
		return
	}
	if _, err := statsDB.Exec(`UPDATE counters SET value = value + 1 WHERE key = ?`, key); err != nil {
		log.Printf("stats increment %s: %v", key, err)
	}
}

func RecordVisit()  { incrementCounter("visits") }
func RecordRoom()   { incrementCounter("rooms_created") }

func RecordChat(durationSeconds int) {
	if statsDB == nil || durationSeconds <= 0 {
		return
	}
	if _, err := statsDB.Exec(`INSERT INTO chat_sessions (duration_seconds) VALUES (?)`, durationSeconds); err != nil {
		log.Printf("stats record chat: %v", err)
	}
}

// PublicStats is what the modal renders — no PII, only aggregates.
type PublicStats struct {
	Visits       int64   `json:"visits"`
	RoomsCreated int64   `json:"rooms_created"`
	ChatsTotal   int64   `json:"chats_total"`
	ChatsWeek    int64   `json:"chats_week"`
	ChatsMonth   int64   `json:"chats_month"`
	ChatsYear    int64   `json:"chats_year"`
	AvgDurAll    float64 `json:"avg_dur_all"`    // seconds
	AvgDurWeek   float64 `json:"avg_dur_week"`
	AvgDurMonth  float64 `json:"avg_dur_month"`
	AvgDurYear   float64 `json:"avg_dur_year"`
	TotalMinutes int64   `json:"total_minutes"`
}

func queryInt(query string, args ...interface{}) int64 {
	var v int64
	statsDB.QueryRow(query, args...).Scan(&v)
	return v
}

func queryFloat(query string, args ...interface{}) float64 {
	var v sql.NullFloat64
	statsDB.QueryRow(query, args...).Scan(&v)
	return v.Float64
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func VisitHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	RecordVisit()
	w.WriteHeader(http.StatusNoContent)
}

type chatPayload struct {
	DurationSeconds int `json:"duration_seconds"`
}

func ChatHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var p chatPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.DurationSeconds <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	RecordChat(p.DurationSeconds)
	w.WriteHeader(http.StatusNoContent)
}

func PublicStatsHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if statsDB == nil {
		http.Error(w, "stats unavailable", http.StatusServiceUnavailable)
		return
	}

	s := PublicStats{
		Visits:       queryInt(`SELECT value FROM counters WHERE key='visits'`),
		RoomsCreated: queryInt(`SELECT value FROM counters WHERE key='rooms_created'`),
		ChatsTotal:   queryInt(`SELECT COUNT(*) FROM chat_sessions`),
		ChatsWeek:    queryInt(`SELECT COUNT(*) FROM chat_sessions WHERE created_at >= datetime('now','-7 days')`),
		ChatsMonth:   queryInt(`SELECT COUNT(*) FROM chat_sessions WHERE created_at >= datetime('now','start of month')`),
		ChatsYear:    queryInt(`SELECT COUNT(*) FROM chat_sessions WHERE created_at >= datetime('now','start of year')`),
		AvgDurAll:    queryFloat(`SELECT COALESCE(AVG(duration_seconds),0) FROM chat_sessions`),
		AvgDurWeek:   queryFloat(`SELECT COALESCE(AVG(duration_seconds),0) FROM chat_sessions WHERE created_at >= datetime('now','-7 days')`),
		AvgDurMonth:  queryFloat(`SELECT COALESCE(AVG(duration_seconds),0) FROM chat_sessions WHERE created_at >= datetime('now','start of month')`),
		AvgDurYear:   queryFloat(`SELECT COALESCE(AVG(duration_seconds),0) FROM chat_sessions WHERE created_at >= datetime('now','start of year')`),
		TotalMinutes: queryInt(`SELECT COALESCE(SUM(duration_seconds),0)/60 FROM chat_sessions`),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
