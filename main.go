package main

import (
	"context"
	"go-react-webrtc/server"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	server.AllRooms = server.RoomMap{}
	server.AllRooms.Init()

	// Start the single broadcaster goroutine here, not per-connection
	go server.Broadcaster()

	// Sweep expired empty rooms every 15 minutes; remove swept IDs from the DB.
	go func() {
		for range time.Tick(15 * time.Minute) {
			if swept := server.AllRooms.SweepExpired(); len(swept) > 0 {
				server.RemovePersistedRooms(swept)
			}
		}
	}()

	// Sync room TTLs to DB every 5 minutes so a restart sees up-to-date expiries.
	go func() {
		for range time.Tick(5 * time.Minute) {
			server.SyncRoomsToDB(server.AllRooms.Snapshot())
		}
	}()

	if err := server.InitStats("stats.db"); err != nil {
		log.Printf("Warning: stats DB unavailable: %v", err)
	}

	// Restore rooms that survived the last restart.
	if persisted, err := server.LoadPersistedRooms(); err != nil {
		log.Printf("Warning: could not load persisted rooms: %v", err)
	} else if len(persisted) > 0 {
		server.AllRooms.Restore(persisted)
		log.Printf("Restored %d room(s) from prior run", len(persisted))
	}

	http.HandleFunc("/create", server.CreateRoomRequestHandler)
	http.HandleFunc("/join", server.JoinRoomRequestHandler)
	http.HandleFunc("/stats/visit", server.VisitHandler)
	http.HandleFunc("/stats/chat", server.ChatHandler)
	http.HandleFunc("/stats/public", server.PublicStatsHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "4242"
	}

	srv := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           nil,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("Starting server on port %s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v\n", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Notify all connected clients before dying so they can reconnect cleanly.
	log.Println("Shutdown signal received — notifying clients...")
	server.AllRooms.BroadcastToAll(map[string]interface{}{"type": "restart", "delay": 3000})
	time.Sleep(3 * time.Second)

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v\n", err)
	}
	log.Println("Server exited gracefully")
}
