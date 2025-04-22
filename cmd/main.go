package main

import (
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/http"
	"github.com/Orfen-0/dash-ads-server/internal/logging"
	"github.com/Orfen-0/dash-ads-server/internal/mqttclient"
	"github.com/Orfen-0/dash-ads-server/internal/rtmp"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
	"log"
)

func main() {

	if err := logging.InitLogger(); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	logger := logging.New("init")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("Failed to load config: %v", err)
	}

	if err != nil {
		logger.Error("Failed to open log file: %v", err)
	}

	// Initialize MinIO storage
	minioStorage, err := storage.NewMinIOStorage(&cfg.Storage)
	if err != nil {
		logger.Error("Failed to initialize MinIO storage: %v", err)
	}

	// Initialize MongoDB
	mongoDB, err := database.NewMongoDB(&cfg.MongoDB)
	if err != nil {
		logger.Error("Failed to initialize MongoDB: %v", err)
	}

	mq, err := mqttclient.NewMQTTClient(&cfg.MQTT, mongoDB)
	if err != nil {
		logger.Error("Failed to init MQTT: %v", err)
	}

	// Create RTMP server
	rtmpServer, err := rtmp.NewServer(&cfg.RTMP, minioStorage, mongoDB)
	if err != nil {
		logger.Error("Failed to create RTMP server: %v", err)
	}

	// Create HTTP server
	httpServer := http.NewServer(&cfg.HTTP, rtmpServer, mq, mongoDB)

	// Start servers
	go func() {
		if err := rtmpServer.Start(); err != nil {
			logger.Error("Failed to start RTMP server: %v", err)
		}
	}()

	if err := httpServer.Start(); err != nil {
		logger.Error("Failed to start HTTP server: %v", err)
	}
}
