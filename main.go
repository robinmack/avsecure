package main

import (
	"context"
	"go-react-webrtc/server"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	server.AllRooms = server.RoomMap{}
	server.AllRooms.Init()

	// Start the single broadcaster goroutine here, not per-connection
	go server.Broadcaster()

	if err := server.InitStats("stats.db"); err != nil {
		log.Printf("Warning: stats DB unavailable: %v", err)
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
		Addr:    ":" + port,
		Handler: nil,
	}

	go func() {
		log.Printf("Starting server on port %s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v\n", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v\n", err)
	}
	log.Println("Server exited gracefully")
}
