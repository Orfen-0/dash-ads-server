package rtmp

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/format/rtmp"
)

type Config struct {
	Port string
	// Add other RTMP-specific configuration fields here
}
type Server struct {
	rtmpConfig *config.RTMPConfig
	storage    *storage.MinIOStorage
	db         *database.MongoDB
}

func NewServer(cfg *config.RTMPConfig, storage *storage.MinIOStorage, db *database.MongoDB) (*Server, error) {
	return &Server{
		rtmpConfig: cfg,
		storage:    storage,
		db:         db,
	}, nil
}

func (s *Server) Start() error {
	server := &rtmp.Server{}

	server.HandlePublish = func(conn *rtmp.Conn) {
		ctx := context.Background()
		streams, _ := conn.Streams()

		fileName := fmt.Sprintf("%s-%d.flv", conn.URL.Path, time.Now().Unix())

		// Create a pipe to stream data
		reader, writer := io.Pipe()

		// Start uploading to MinIO
		go func() {
			defer writer.Close()
			err := s.storage.UploadStream(ctx, fileName, reader)
			if err != nil {
				log.Printf("Failed to upload stream: %v", err)
			}
		}()

		// Write stream data to the pipe
		go func() {
			err := avutil.CopyFile(writer, conn)
			if err != nil {
				log.Printf("Error copying stream: %v", err)
			}
		}()

		log.Printf("Publishing stream: %s", conn.URL.Path)

		// Here you could add metadata to MongoDB
		// s.db.SaveStreamMetadata(...)
	}

	server.HandlePlay = func(conn *rtmp.Conn) {
		log.Printf("Play stream: %s", conn.URL.Path)
		// Implement your play logic here if needed
	}

	addr := fmt.Sprintf(":%s", s.rtmpConfig.Port)
	log.Printf("RTMP server starting on %s", addr)
	return server.ListenAndServe(addr)
}
