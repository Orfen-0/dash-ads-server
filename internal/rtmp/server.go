package rtmp

import (
	"context"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/format/flv"
	"github.com/nareix/joy4/format/rtmp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"log"
	"strconv"
	"strings"
	"time"
)

type ClientConn struct {
	Conn       *rtmp.Conn
	PacketChan chan av.Packet
}

type Server struct {
	rtmpConfig        *config.RTMPConfig
	Storage           *storage.MinIOStorage
	db                *database.MongoDB
	LiveStreamManager *LiveStreamManager
}

func NewServer(cfg *config.RTMPConfig, storage *storage.MinIOStorage, db *database.MongoDB) (*Server, error) {
	return &Server{
		rtmpConfig:        cfg,
		Storage:           storage,
		db:                db,
		LiveStreamManager: NewLiveStreamManager(),
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

	// Extract parameters
	latStr := conn.URL.Query().Get("lat")
	lngStr := conn.URL.Query().Get("lng")
	acc := conn.URL.Query().Get("acc")
	deviceID := conn.URL.Query().Get("deviceId")
	timestamp := conn.URL.Query().Get("ts")
	eventIDStr := conn.URL.Query().Get("eventId")
	if deviceID == "" {
		return fmt.Errorf("deviceId is required in URL parameters")
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	parsedTime := time.Unix(sec/1000, (sec%1000)*int64(time.Millisecond))

	// Create stream record
	streamID := primitive.NewObjectID()
	streamIDStr := streamID.Hex()
	rtmpURL := fmt.Sprintf("rtmp://%s:%s/live/%s", s.rtmpConfig.Domain, s.rtmpConfig.Port, streamIDStr)
	playbackURL := fmt.Sprintf("http://%s:%s/playback/%s.flv", s.rtmpConfig.Domain, s.rtmpConfig.HTTPPort, streamIDStr)

	// Parse lat/lng/acc
	latVal, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		latVal = 0.0
		log.Printf("Warning: invalid latStr=%q, defaulting to 0", latStr)
	}
	lngVal, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		lngVal = 0.0
		log.Printf("Warning: invalid lngStr=%q, defaulting to 0", lngStr)
	}
	accVal, err := strconv.ParseFloat(acc, 32)
	if err != nil {
		accVal = 0.0
		log.Printf("Warning: invalid acc=%q, defaulting to 0", acc)
	}

	stream := &database.Stream{
		ID:          streamID,
		DeviceId:    deviceID,
		Title:       deviceID,
		Description: latStr + "_" + lngStr,
		StartTime:   parsedTime,
		Status:      "live",
		RTMPURL:     rtmpURL,
		PlaybackURL: playbackURL,
		StartLocation: database.GeoJSONPoint{
			Type:        "Point",
			Coordinates: [2]float64{lngVal, latVal}, // [lng, lat]
		},
		LocAccuracy: float32(accVal),
	}

	if eventIDStr != "" {
		eID, err := primitive.ObjectIDFromHex(eventIDStr)
		if err == nil {
			stream.EventID = &eID
		}
	}
	// Save to database
	if err := s.db.CreateStream(stream); err != nil {
		return fmt.Errorf("failed to create stream record: %w", err)
	}

	streams, err := conn.Streams()
	if err != nil {
		return fmt.Errorf("error getting codec data: %w", err)
	}

	s.LiveStreamManager.AddPublisher(streamIDStr, conn, streams)
	defer s.LiveStreamManager.RemovePublisher(streamIDStr)

	pipeReader, pipeWriter := io.Pipe()
	defer pipeWriter.Close()
	flvMuxer := flv.NewMuxer(pipeWriter)

	if err := flvMuxer.WriteHeader(streams); err != nil {
		return fmt.Errorf("error writing FLV header: %w", err)
	}

	// Start MinIO upload
	go func() {
		defer pipeReader.Close()
		objectPath := fmt.Sprintf("streams/%s/%s.flv", deviceID, streamIDStr)
		if err := s.Storage.UploadStream(context.Background(), objectPath, pipeReader); err != nil {
			log.Printf("Failed to upload stream to MinIO: %v", err)
		}
	}()

	for {
		packet, err := conn.ReadPacket()
		if err != nil {
			if err == io.EOF {
				log.Printf("Stream %s ended", streamIDStr)

				s.LiveStreamManager.RemovePublisher(streamIDStr)
				s.LiveStreamManager.CloseAllClients(streamIDStr)

				break
			}
			return fmt.Errorf("error reading packet: %w", err)
		}

		if err := flvMuxer.WritePacket(packet); err != nil {
			return fmt.Errorf("error writing packet to FLV: %w", err)
		}

		s.LiveStreamManager.ForwardPacket(streamIDStr, packet)
	}

	if err := s.db.EndStream(stream.ID); err != nil {
		log.Printf("Failed to mark stream as ended: %v", err)
	}

	return nil
}
func (s *Server) handlePlay(conn *rtmp.Conn) error {
	// Extract the stream ID from the path:
	streamIDStr := strings.TrimPrefix(conn.URL.Path, "/live/")
	if streamIDStr == "" || streamIDStr == conn.URL.Path {
		return fmt.Errorf("invalid stream path or missing stream ID")
	}

	// Look up MongoDB to see if status == live or ended
	objectID, err := primitive.ObjectIDFromHex(streamIDStr)
	if err != nil {
		return fmt.Errorf("invalid ObjectID: %w", err)
	}
	dbStream, err := s.db.GetStream(objectID)
	if err != nil {
		return fmt.Errorf("failed to get stream: %w", err)
	}

	if dbStream.Status == "live" {
		// 1) Get the live stream info
		ls := s.LiveStreamManager.GetPublisher(streamIDStr)
		if ls == nil {
			return fmt.Errorf("no live stream found for ID %s", streamIDStr)
		}

		// 2) Create a client with its own packet channel
		client := &ClientConn{
			Conn:       conn,
			PacketChan: make(chan av.Packet, 50), // Adjust buffer size as needed
		}
		// Add the client to the manager
		s.LiveStreamManager.AddRTMPClient(streamIDStr, conn)
		defer s.LiveStreamManager.RemoveRTMPClient(streamIDStr, conn)

		// 3) Write FLV header (the same streams from the publisher)
		if err := conn.WriteHeader(ls.Streams); err != nil {
			return fmt.Errorf("failed to write FLV header: %w", err)
		}

		// 4) Continuously read from PacketChan and forward to this client
		for {
			packet, ok := <-client.PacketChan
			if !ok {
				log.Printf("Client channel closed for stream %s", streamIDStr)
				break
			}
			// Send packet to the RTMP client
			if err := conn.WritePacket(packet); err != nil {
				log.Printf("Failed to write packet to client for stream %s: %v", streamIDStr, err)
				return err
			}
		}

		return nil

	} else if dbStream.Status == "ended" {
		// Recorded playback from MinIO
		log.Printf("Playing recorded stream: %s", streamIDStr)
		objectPath := fmt.Sprintf("streams/%s/%s.flv", dbStream.DeviceId, dbStream.ID.Hex())

		flvFileReader, err := s.Storage.DownloadStream(context.Background(), objectPath)
		if err != nil {
			return fmt.Errorf("failed to retrieve stream file: %w", err)
		}
		defer flvFileReader.Close()

		demuxer := flv.NewDemuxer(flvFileReader)
		streams, err := demuxer.Streams()
		if err != nil {
			return fmt.Errorf("failed to parse FLV streams: %w", err)
		}
		if err := conn.WriteHeader(streams); err != nil {
			return fmt.Errorf("failed to write RTMP header: %w", err)
		}

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
		return nil
	}

	return fmt.Errorf("invalid stream status: %s", dbStream.Status)
}
