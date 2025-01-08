package main

import (
	"log"

	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/http"
	"github.com/Orfen-0/dash-ads-server/internal/rtmp"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize MinIO storage
	minioStorage, err := storage.NewMinIOStorage(&cfg.Storage)
	if err != nil {
		log.Fatalf("Failed to initialize MinIO storage: %v", err)
	}

	// Initialize MongoDB
	mongoDB, err := database.NewMongoDB(&cfg.MongoDB)
	if err != nil {
		log.Fatalf("Failed to initialize MongoDB: %v", err)
	}

	// Create RTMP server
	rtmpServer, err := rtmp.NewServer(&cfg.RTMP, minioStorage, mongoDB)
	if err != nil {
		log.Fatalf("Failed to create RTMP server: %v", err)
	}

	// Create HTTP server
	httpServer := http.NewServer(&cfg.HTTP, rtmpServer, mongoDB)

	// Start servers
	go func() {
		if err := rtmpServer.Start(); err != nil {
			log.Fatalf("Failed to start RTMP server: %v", err)
		}
	}()

	if err := httpServer.Start(); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}
