package rtmp

import (
	"context"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
	"github.com/nareix/joy4/format/flv"
	"github.com/nareix/joy4/format/rtmp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"log"
	"strconv"
	"time"
)

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
	server := &rtmp.Server{
		Addr: ":" + s.rtmpConfig.Port,
	}

	server.HandlePublish = func(conn *rtmp.Conn) {
		if err := s.handlePublish(conn); err != nil {
			log.Printf("Error handling publish: %v", err)
		}
	}

	server.HandlePlay = func(conn *rtmp.Conn) {
		if err := s.handlePlay(conn); err != nil {
			log.Printf("Error handling play: %v", err)
		}
	}

	log.Printf("RTMP server starting on %s", server.Addr)
	return server.ListenAndServe()
}

func (s *Server) handlePublish(conn *rtmp.Conn) error {
	streams, err := conn.Streams()
	if err != nil {
		return fmt.Errorf("error getting streams: %w", err)
	}

	// Extract user ID from URL parameters
	latStr := conn.URL.Query().Get("lat")
	lngStr := conn.URL.Query().Get("lng")
	acc := conn.URL.Query().Get("acc")
	deviceID := conn.URL.Query().Get("deviceId")
	timestamp := conn.URL.Query().Get("ts")
	if deviceID == "" {
		return fmt.Errorf("deviceId  is required in URL parameters")
	}

	if err != nil {
		return fmt.Errorf("invalid userId format: %w", err)
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	parsedTime, err := time.Unix(sec, 0), nil
	if err != nil {
		return fmt.Errorf("invalid timestamp parsing")
	}
	// Create stream record
	streamID := primitive.NewObjectID()
	deviceIDObjId, err := primitive.ObjectIDFromHex(deviceID)

	fileName := fmt.Sprintf("%s.flv", streamID.Hex())
	rtmpURL := fmt.Sprintf("rtmp://%s/%s", s.rtmpConfig.Domain, streamID.Hex())
	playbackURL := fmt.Sprintf("http://%s/play/%s", s.rtmpConfig.Domain, streamID.Hex())

	stream := &database.Stream{
		ID:               streamID,
		DeviceIdObjectId: deviceIDObjId,
		DeviceId:         deviceID,
		Title:            deviceID,
		Description:      latStr + "_" + lngStr,
		StartTime:        parsedTime,
		Status:           "live",
		RTMPURL:          rtmpURL,
		PlaybackURL:      playbackURL,
		Latitude:         latStr,
		Longitude:        lngStr,
		LocAccuracy:      acc,
	}

	// Save stream to MongoDB
	if err := s.db.CreateStream(stream); err != nil {
		return fmt.Errorf("failed to create stream record: %w", err)
	}

	// Create pipe for streaming to MinIO
	pipeReader, pipeWriter := io.Pipe()
	flvMuxer := flv.NewMuxer(pipeWriter)

	// Start MinIO upload
	go func() {
		defer pipeReader.Close()
		objectPath := fmt.Sprintf("streams/%s/%s", stream.DeviceIdObjectId.Hex(), fileName)
		if err := s.storage.UploadStream(context.Background(), objectPath, pipeReader); err != nil {
			log.Printf("Failed to upload stream to MinIO: %v", err)
		}
	}()

	// Write FLV header
	if err := flvMuxer.WriteHeader(streams); err != nil {
		return fmt.Errorf("error writing FLV header: %w", err)
	}

	// Handle stream end
	defer func() {
		pipeWriter.Close()
		if err := s.db.EndStream(stream.ID); err != nil {
			log.Printf("Failed to mark stream as ended: %v", err)
		}
	}()

	// Copy packets
	for {
		packet, err := conn.ReadPacket()
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("error reading packet: %w", err)
			}
			return nil
		}

		if err := flvMuxer.WritePacket(packet); err != nil {
			return fmt.Errorf("error writing packet to FLV: %w", err)
		}
	}
}

func (s *Server) handlePlay(conn *rtmp.Conn) error {
	// Extract stream ID from path
	streamIDStr := conn.URL.Path
	if streamIDStr == "" {
		return fmt.Errorf("stream ID not provided")
	}

	streamID, err := primitive.ObjectIDFromHex(streamIDStr)
	if err != nil {
		return fmt.Errorf("invalid stream ID format: %w", err)
	}

	// Get stream info
	stream, err := s.db.GetStream(streamID)
	if err != nil {
		return fmt.Errorf("failed to get stream: %w", err)
	}

	if stream.Status != "live" {
		return fmt.Errorf("stream is not live")
	}

	// Here you would implement the playback logic
	// This might involve reading from MinIO and sending to the RTMP client
	// For now, we'll just log the attempt
	log.Printf("Play request for stream: %s", stream.ID.Hex())

	return nil
}
