package main

import (
	"github.com/Orfen-0/dash-ads-server/internal/mqttclient"
	"io"
	"log"
	"os"

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

	logFile, err := os.OpenFile("logs/server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	multi := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multi)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

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

	mq, err := mqttclient.NewMQTTClient(&cfg.MQTT, mongoDB)
	if err != nil {
		log.Fatalf("Failed to init MQTT: %v", err)
	}

	// Create RTMP server
	rtmpServer, err := rtmp.NewServer(&cfg.RTMP, minioStorage, mongoDB)
	if err != nil {
		log.Fatalf("Failed to create RTMP server: %v", err)
	}

	// Create HTTP server
	httpServer := http.NewServer(&cfg.HTTP, rtmpServer, mq, mongoDB)

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
