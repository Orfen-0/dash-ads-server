package http

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/mqttclient"
	"github.com/Orfen-0/dash-ads-server/internal/rtmp"
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/format/flv"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	config     *config.HTTPConfig
	httpServer *http.Server
	rtmpServer *rtmp.Server
	db         *database.MongoDB
}

type Config struct {
	Port string
}

type LocationUpdateRequest struct {
	DeviceID  string  `json:"deviceId"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float32 `json:"accuracy"`
	Timestamp int64   `json:"timestamp"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float32 `json:"accuracy"`
	Timestamp int64   `json:"timestamp"`
}

type DeviceRegistrationRequest struct {
	DeviceID     string   `json:"deviceId"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
	OsVersion    string   `json:"osVersion"`
	Location     Location `json:"location"`
}

type CreateEventRequest struct {
	TriggeredBy string  `json:"triggeredBy"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Radius      float64 `json:"radius"`
}

type DeviceStatusResponse struct {
	IsRegistered bool `json:"isRegistered"`
}

func NewServer(config *config.HTTPConfig, rtmpServer *rtmp.Server, mq *mqttclient.MQTTClient, db *database.MongoDB) *Server {
	return &Server{
		config:     config,
		rtmpServer: rtmpServer,
		db:         db,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Apply middleware
	handler := s.loggingMiddleware(s.corsMiddleware(mux))

	// Set up routes
	mux.HandleFunc("/health", s.healthCheckHandler)
	mux.HandleFunc("/streams", s.streamsHandler)
	mux.HandleFunc("/devices/register", s.registerDeviceHandler)
	mux.HandleFunc("/devices/", s.deviceStatusHandler)
	mux.HandleFunc("/playback/", s.handleHTTPPlayFLV)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			s.createEventHandler(w, r)
		case "GET":
			s.listEventsHandler(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	s.httpServer = &http.Server{
		Addr:    ":" + s.config.Port,
		Handler: handler,
	}

	log.Printf("Starting HTTP server on port %s", s.config.Port)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Middleware
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")

		if r.Method == "OPTIONS" {
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Handlers
func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func (s *Server) streamsHandler(w http.ResponseWriter, r *http.Request) {
	//streams := s.rtmpServer.GetActiveStreams() // You'll need to implement this in your RTMP server
	//w.Header().Set("Content-Type", "application/json")
	//json.NewEncoder(w).Encode(streams)
}

func (s *Server) registerDeviceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeviceRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	device := &database.Device{
		DeviceID:     req.DeviceID,
		Model:        req.Model,
		Manufacturer: req.Manufacturer,
		OsVersion:    req.OsVersion,
		LastLocation: database.GeoJSONPoint{
			Type:        "Point",
			Coordinates: [2]float64{req.Location.Longitude, req.Location.Latitude},
		},
		LastUpdatedAt: time.Unix(req.Location.Timestamp, 0),
		LocAccuracy:   req.Location.Accuracy,
	}

	if err := s.db.RegisterDevice(device); err != nil {
		log.Printf("Error registering device: %v", err)
		http.Error(w, "Failed to register device", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Device registered successfully",
	}

	sendJSONResponse(w, http.StatusOK, response)
}

func (s *Server) deviceStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	deviceID := strings.TrimPrefix(r.URL.Path, "/devices/")
	deviceID = strings.TrimSuffix(deviceID, "/status")

	if deviceID == "" {
		http.Error(w, "Device ID is required", http.StatusBadRequest)
		return
	}

	_, err := s.db.GetDeviceByID(deviceID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			sendJSONResponse(w, http.StatusOK, DeviceStatusResponse{
				IsRegistered: false,
			})
			return
		}

		log.Printf("Error checking device status: %v", err)
		http.Error(w, "Failed to check device status", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, http.StatusOK, DeviceStatusResponse{
		IsRegistered: true,
	})
}

func (s *Server) createEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Radius == 0 {
		req.Radius = 2.0
	}
	// Build the event doc for DB
	evt := &database.Event{
		ID:          primitive.NewObjectID(),
		TriggeredBy: req.TriggeredBy,
		StartTime:   time.Now(),
		Location: database.EventLocation{
			Latitude:  req.Latitude,
			Longitude: req.Longitude,
		},
		Radius: req.Radius,
		Status: "active",
	}

	// Insert event in DB
	if err := s.db.CreateEvent(evt); err != nil {
		log.Printf("Error creating event: %v", err)
		http.Error(w, "Failed to create event", http.StatusInternalServerError)
		return
	}

	// (Optional) you might find nearby devices + push "start stream" commands here
	// e.g.: s.pushStartStreamToNearbyDevices(evt)

	// Return the newly created event ID
	sendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"eventId": evt.ID.Hex(),
		"status":  evt.Status,
		"message": "Event created successfully",
	})
}

func (s *Server) listEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse optional query params ?from=YYYY-MM-DD&to=YYYY-MM-DD
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	var fromTime, toTime time.Time
	var err error

	if fromStr != "" {
		fromTime, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			http.Error(w, "Invalid 'from' date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if toStr != "" {
		toTime, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			http.Error(w, "Invalid 'to' date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// Query DB for events
	events, err := s.db.ListEventsByDateRange(fromTime, toTime)
	if err != nil {
		log.Printf("Error listing events: %v", err)
		http.Error(w, "Failed to list events", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, http.StatusOK, events)
}

func (s *Server) handleHTTPPlayFLV(w http.ResponseWriter, r *http.Request) {
	// Expected URL: /playback/{streamID}.flv
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".flv") {
		http.Error(w, "Invalid playback URL", http.StatusBadRequest)
		return
	}

	streamIDStr := strings.TrimSuffix(parts[2], ".flv")

	// Validate stream ID
	objectID, err := primitive.ObjectIDFromHex(streamIDStr)
	if err != nil {
		http.Error(w, "Invalid stream ID", http.StatusBadRequest)
		return
	}

	// Retrieve stream information from the database
	dbStream, err := s.db.GetStream(objectID)
	if err != nil {
		http.Error(w, "Stream not found", http.StatusNotFound)
		return
	}

	switch dbStream.Status {
	case "live":
		s.serveLiveFLV(w, r, streamIDStr, dbStream)
	case "ended":
		s.serveRecordedFLV(w, r, dbStream)
	default:
		http.Error(w, "Invalid stream status", http.StatusBadRequest)
	}
}

func (s *Server) serveRecordedFLV(w http.ResponseWriter, r *http.Request, dbStream *database.Stream) {
	objectPath := fmt.Sprintf("streams/%s/%s.flv", dbStream.DeviceId, dbStream.ID.Hex())

	flvFileReader, err := s.rtmpServer.Storage.DownloadStream(context.Background(), objectPath)
	if err != nil {
		http.Error(w, "Failed to retrieve stream file", http.StatusInternalServerError)
		return
	}
	defer flvFileReader.Close()

	w.Header().Set("Content-Type", "video/x-flv")
	// Optionally set caching headers
	w.Header().Set("Cache-Control", "no-cache")

	// Stream the FLV file to the client
	if _, err := io.Copy(w, flvFileReader); err != nil {
		log.Printf("Error streaming recorded FLV: %v", err)
	}
}

func (s *Server) serveLiveFLV(w http.ResponseWriter, r *http.Request, streamIDStr string, dbStream *database.Stream) {
	// Retrieve the live stream from the RTMP server's LiveStreamManager
	ls := s.rtmpServer.LiveStreamManager.GetPublisher(streamIDStr)
	if ls == nil {
		http.Error(w, "No live stream found", http.StatusNotFound)
		return
	}

	// Set the appropriate headers
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")

	// Use Flush to send data incrementally
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create a channel for sending packets to this HTTP-FLV client
	clientChan := make(chan av.Packet, 100)
	s.rtmpServer.LiveStreamManager.AddHTTPClient(streamIDStr, clientChan)
	defer s.rtmpServer.LiveStreamManager.RemoveHTTPClient(streamIDStr, clientChan)

	// Create FLV muxer to write packets to the HTTP response
	flvMuxer := flv.NewMuxer(w)
	// Write FLV header to the HTTP response
	if err := flvMuxer.WriteHeader(ls.Streams); err != nil {
		log.Printf("Failed to write FLV header to HTTP-FLV client: %v", err)
		return
	}
	flusher.Flush()

	// Stream packets to the client
	for packet := range clientChan {
		if err := flvMuxer.WritePacket(packet); err != nil {
			log.Printf("Failed to write FLV packet to HTTP-FLV client: %v", err)
			break
		}
		flusher.Flush()
	}
}

func sendJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
