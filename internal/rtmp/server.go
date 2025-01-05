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
	"strings"
	"time"
)

type Server struct {
	rtmpConfig        *config.RTMPConfig
	storage           *storage.MinIOStorage
	db                *database.MongoDB
	liveStreamManager *LiveStreamManager
}

func NewServer(cfg *config.RTMPConfig, storage *storage.MinIOStorage, db *database.MongoDB) (*Server, error) {
	return &Server{
		rtmpConfig:        cfg,
		storage:           storage,
		db:                db,
		liveStreamManager: NewLiveStreamManager(),
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
	log.Printf("New publish request from %s", conn.URL)
	streams, err := conn.Streams()
	if err != nil {
		return fmt.Errorf("error getting streams: %w", err)
	}
	log.Printf("incoming stream")
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
	parsedTime := time.Unix(sec/1000, (sec%1000)*int64(time.Millisecond))
	if err != nil {
		return fmt.Errorf("invalid timestamp parsing")
	}
	// Create stream record
	streamID := primitive.NewObjectID()
	fileName := fmt.Sprintf("%s.flv", streamID.Hex())
	rtmpURL := fmt.Sprintf("rtmp://%s:%s/live/%s", s.rtmpConfig.Domain, s.rtmpConfig.Port, streamID.Hex())
	playbackURL := fmt.Sprintf("http://%s:%s/playback/%s.flv", s.rtmpConfig.Domain, s.rtmpConfig.HTTPPort, streamID.Hex())
	log.Printf(rtmpURL)
	stream := &database.Stream{
		ID:          streamID,
		DeviceId:    deviceID,
		Title:       deviceID,
		Description: latStr + "_" + lngStr,
		StartTime:   parsedTime,
		Status:      "live",
		RTMPURL:     rtmpURL,
		PlaybackURL: playbackURL,
		Latitude:    latStr,
		Longitude:   lngStr,
		LocAccuracy: acc,
	}

	// Save stream to MongoDB
	if err := s.db.CreateStream(stream); err != nil {
		return fmt.Errorf("failed to create stream record: %w", err)
	}

	streamIDStr := streamID.String()
	s.liveStreamManager.AddPublisher(streamIDStr, conn)
	defer s.liveStreamManager.RemovePublisher(streamIDStr)

	// Create pipe for streaming to MinIO
	pipeReader, pipeWriter := io.Pipe()
	flvMuxer := flv.NewMuxer(pipeWriter)
	if err := flvMuxer.WriteHeader(streams); err != nil {
		return fmt.Errorf("error writing FLV header: %w", err)
	}
	// Start MinIO upload
	go func() {
		defer pipeReader.Close()
		objectPath := fmt.Sprintf("%s/%s/%s", s.storage.Bucket, stream.DeviceId, fileName)
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

	streamIDStr := strings.TrimPrefix(conn.URL.Path, "/live/")
	if streamIDStr == "" || streamIDStr == conn.URL.Path {
		return fmt.Errorf("invalid stream path or missing stream ID")
	}
	if streamIDStr == "" {
		return fmt.Errorf("stream ID not provided")
	}
	log.Printf("incoming request for %s stream", streamIDStr)
	// Fetch stream metadata from DB
	streamId, _ := primitive.ObjectIDFromHex(streamIDStr)
	stream, err := s.db.GetStream(streamId)
	if err != nil {
		return fmt.Errorf("failed to get stream: %w", err)
	}

	if stream.Status == "live" {
		// For live streams, forward packets from the publishing connection
		// Check if the stream is live
		publisher := s.liveStreamManager.GetPublisher(streamIDStr)
		if publisher != nil {
			log.Printf("Client connected to live stream: %s", streamIDStr)

			// Add the client to the LiveStreamManager
			s.liveStreamManager.AddClient(streamIDStr, conn)
			defer s.liveStreamManager.RemoveClient(streamIDStr, conn)

			// Forward packets from the publisher
			for {
				packet, err := publisher.ReadPacket()
				if err != nil {
					if err == io.EOF {
						log.Printf("Live stream %s ended", streamIDStr)
						break
					}
					log.Printf("Error reading live packet for stream %s: %v", streamIDStr, err)
					return err
				}

				if err := conn.WritePacket(packet); err != nil {
					log.Printf("Error forwarding packet to client for stream %s: %v", streamIDStr, err)
					return err
				}
			}

			return nil
		}
	} else if stream.Status == "ended" {
		// For recorded streams, retrieve the .flv file from MinIO
		log.Printf("Playing recorded stream: %s", streamIDStr)
		objectPath := fmt.Sprintf("streams/%s/%s", stream.DeviceId, stream.ID.Hex()+".flv")
		flvFileReader, err := s.storage.DownloadStream(context.Background(), objectPath)
		if err != nil {
			return fmt.Errorf("failed to retrieve stream file: %w", err)
		}
		defer flvFileReader.Close()

		// Parse the FLV file
		demuxer := flv.NewDemuxer(flvFileReader)
		streams, err := demuxer.Streams()
		if err != nil {
			return fmt.Errorf("failed to parse FLV streams: %w", err)
		}

		// Write FLV header to the RTMP connection
		if err := conn.WriteHeader(streams); err != nil {
			return fmt.Errorf("failed to write RTMP header: %w", err)
		}

		// Write packets to the RTMP connection
		for {
			packet, err := demuxer.ReadPacket()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read FLV packet: %w", err)
			}

			if err := conn.WritePacket(packet); err != nil {
				return fmt.Errorf("failed to write packet to RTMP client: %w", err)
			}
		}
	} else {
		return fmt.Errorf("invalid stream status: %s", stream.Status)
	}

	return nil
}
