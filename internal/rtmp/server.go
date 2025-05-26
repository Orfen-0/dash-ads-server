package rtmp

import (
	"context"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/logging"
	"github.com/Orfen-0/dash-ads-server/internal/storage"
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/format/flv"
	"github.com/nareix/joy4/format/rtmp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
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

var logger = logging.New("rtmp")

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
			logger.Error("Error handling publish", "err", err)
		}
	}

	server.HandlePlay = func(conn *rtmp.Conn) {
		if err := s.handlePlay(conn); err != nil {
			logger.Error("Error handling play", "err", err)
		}
	}

	logger.Info("RTMP server starting on %s", server.Addr)
	return server.ListenAndServe()
}

func (s *Server) handlePublish(conn *rtmp.Conn) error {
	logger.Info("New publish request from %s", conn.URL)
	lastLogTime := time.Now()

	// Extract parameters
	latStr := conn.URL.Query().Get("lat")
	lngStr := conn.URL.Query().Get("lng")
	acc := conn.URL.Query().Get("acc")
	deviceID := conn.URL.Query().Get("deviceId")
	timestamp := conn.URL.Query().Get("ts")
	eventIDStr := conn.URL.Query().Get("eventId")
	if deviceID == "" {
		logger.Error("deviceId is required in URL parameters")
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		logger.Error("invalid timestamp")
		return err
	}
	parsedTime := time.Unix(sec/1000, (sec%1000)*int64(time.Millisecond))
	deviceStreamStartTs, err := strconv.ParseInt(timestamp, 10, 64)
	logger.Info("Incoming stream request", "deviceId", deviceID, "eventId", eventIDStr)
	// Create stream record
	streamID := primitive.NewObjectID()
	streamIDStr := streamID.Hex()
	rtmpURL := fmt.Sprintf("rtmp://%s:%s/live/%s", s.rtmpConfig.Domain, s.rtmpConfig.Port, streamIDStr)
	playbackURL := fmt.Sprintf("http://%s:%s/playback/%s.flv", s.rtmpConfig.Domain, s.rtmpConfig.HTTPPort, streamIDStr)

	// Parse lat/lng/acc
	latVal, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		latVal = 0.0
		logger.Warn("Warning: invalid latStr=%q, defaulting to 0", latStr)
	}
	lngVal, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		lngVal = 0.0
		logger.Warn("Warning: invalid lngStr=%q, defaulting to 0", lngStr)
	}
	accVal, err := strconv.ParseFloat(acc, 32)
	if err != nil {
		accVal = 0.0
		logger.Warn("Warning: invalid acc=%q, defaulting to 0", acc)
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
			logger.Info("Associated stream with event id", "eventId", eID.Hex())

		}
	}
	// Save to database
	if err := s.db.CreateStream(stream); err != nil {
		logger.Error("failed to create stream record", "err", err)
		return err
	}
	logger.Info("Stream document created", "streamId", stream.ID.Hex(), "deviceId", deviceID)

	streams, err := conn.Streams()
	if err != nil {
		logger.Error("error getting codec data", "err", err)
		return err
	}

	s.LiveStreamManager.AddPublisher(streamIDStr, conn, streams)
	defer s.LiveStreamManager.RemovePublisher(streamIDStr)

	pipeReader, pipeWriter := io.Pipe()
	defer pipeWriter.Close()
	flvMuxer := flv.NewMuxer(pipeWriter)
	logger.Info("Stream setup complete. Starting packet loop", "streamId", streamIDStr)

	if err := flvMuxer.WriteHeader(streams); err != nil {
		logger.Error("error writing FLV header", "err", err)
		return err
	}

	// Start MinIO upload
	go func() {
		logger.Info("Upload initiated", "deviceId", deviceID, "streamId", streamIDStr)
		defer pipeReader.Close()
		objectPath := fmt.Sprintf("streams/%s/%s.flv", deviceID, streamIDStr)
		if err := s.Storage.UploadStream(context.Background(), objectPath, pipeReader); err != nil {
			logger.Error("Failed to upload stream to MinIO", "err", err)
		}
		logger.Info("[MinIO] Stream uploaded successfully", "deviceId", deviceID, "streamId", streamIDStr)

	}()
	packetCount := 0
	for {
		packetCount++
		packet, err := conn.ReadPacket()
		if err != nil {
			if err == io.EOF {
				logger.Info("[RTMP] Stream ended cleanly", "devideId", deviceID, streamIDStr)

				s.LiveStreamManager.RemovePublisher(streamIDStr)
				s.LiveStreamManager.CloseAllClients(streamIDStr)

				break
			}
			logger.Error("error reading packet", "err", err)
			return err
		}

		if err := flvMuxer.WritePacket(packet); err != nil {
			logger.Error("error writing packet to FLV", "err", err)
			return err
		}

		s.LiveStreamManager.ForwardPacket(streamIDStr, packet)
		if time.Since(lastLogTime) >= 5*time.Second {
			tNow := time.Now().UnixMilli()
			streamTimeMs := int64(packet.Time)
			expectedServerTs := deviceStreamStartTs + streamTimeMs
			latencyMs := tNow - expectedServerTs

			logger.Info("[RTMP] Stream packet received",
				"timestamp", tNow,
				"deviceId", deviceID,
				"streamId", streamIDStr,
				"streamTime", streamTimeMs,
				"expected_ts", expectedServerTs,
				"latency_ms", latencyMs,
				"isKeyFrame", packet.IsKeyFrame,
				"sizeBytes", len(packet.Data),
				"packetsSinceLast", packetCount,
			)
			lastLogTime = time.Now()
			packetCount = 0
		}

	}

	if err := s.db.EndStream(stream.ID); err != nil {
		logger.Error("Failed to mark stream as ended", "err", err)
	}
	logger.Info("Stream marked as ended", "deviceId", deviceID, "streamId", streamIDStr)

	return nil
}
func (s *Server) handlePlay(conn *rtmp.Conn) error {
	// Extract the stream ID from the path:
	streamIDStr := strings.TrimPrefix(conn.URL.Path, "/live/")
	if streamIDStr == "" || streamIDStr == conn.URL.Path {
		logger.Error("invalid stream path or missing stream ID")
		return nil
	}

	// Look up MongoDB to see if status == live or ended
	objectID, err := primitive.ObjectIDFromHex(streamIDStr)
	if err != nil {
		logger.Error("invalid ObjectID", "err", err)
		return err
	}
	dbStream, err := s.db.GetStream(objectID)
	if err != nil {
		logger.Error("failed to get stream", "err", err)
		return err
	}

	if dbStream.Status == "live" {
		// 1) Get the live stream info
		ls := s.LiveStreamManager.GetPublisher(streamIDStr)
		if ls == nil {
			logger.Error("no live stream found for ID", "streamId", streamIDStr)
			return err
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
			logger.Error("failed to write FLV header", "err", err)
			return err
		}

		// 4) Continuously read from PacketChan and forward to this client
		for {
			packet, ok := <-client.PacketChan
			if !ok {
				logger.Info("Client channel closed for stream", "streamId", streamIDStr)
				break
			}
			// Send packet to the RTMP client
			if err := conn.WritePacket(packet); err != nil {
				logger.Error("Failed to write packet to client for stream", "streamId", streamIDStr, "err", err)
				return err
			}
		}

		return nil

	} else if dbStream.Status == "ended" {
		// Recorded playback from MinIO
		logger.Info("Playing recorded stream", "streamId", streamIDStr)
		objectPath := fmt.Sprintf("streams/%s/%s.flv", dbStream.DeviceId, dbStream.ID.Hex())

		flvFileReader, err := s.Storage.DownloadStream(context.Background(), objectPath)
		if err != nil {
			logger.Error("failed to retrieve stream file", "err", err)
			return err
		}
		defer flvFileReader.Close()

		demuxer := flv.NewDemuxer(flvFileReader)
		streams, err := demuxer.Streams()
		if err != nil {
			logger.Error("failed to parse FLV streams", "err", err)
			return err
		}
		if err := conn.WriteHeader(streams); err != nil {
			logger.Error("failed to write RTMP header", "err", err)
			return err
		}

		for {
			packet, err := demuxer.ReadPacket()
			if err == io.EOF {
				break
			}
			if err != nil {
				logger.Error("failed to read FLV packet", "err", err)
				return err
			}
			if err := conn.WritePacket(packet); err != nil {
				logger.Error("failed to write packet to RTMP client", "err", err)
				return err
			}
		}
		return nil
	}
	logger.Error("invalid stream status", "status", dbStream.Status)
	return err
}
