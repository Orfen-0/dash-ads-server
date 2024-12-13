package http

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"github.com/Orfen-0/dash-ads-server/internal/rtmp"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	config     *config.HTTPConfig
	httpServer *http.Server
	rtmpServer *rtmp.Server
	db         *database.MongoDB // Add this line

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

type DeviceStatusResponse struct {
	IsRegistered bool `json:"isRegistered"`
}

func NewServer(config *config.HTTPConfig, rtmpServer *rtmp.Server, db *database.MongoDB) *Server {
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
	mux.HandleFunc("/devices/location", s.updateLocationHandler)
	mux.HandleFunc("/devices/", s.deviceStatusHandler) // Note the trailing slash

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
		LastLocation: database.DeviceLocation{
			Latitude:  req.Location.Latitude,
			Longitude: req.Location.Longitude,
			Accuracy:  req.Location.Accuracy,
			Timestamp: req.Location.Timestamp,
		},
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

func (s *Server) updateLocationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LocationUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	location := database.DeviceLocation{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Accuracy:  req.Accuracy,
		Timestamp: req.Timestamp,
	}

	if err := s.db.UpdateDeviceLocation(req.DeviceID, location); err != nil {
		log.Printf("Error updating location: %v", err)
		http.Error(w, "Failed to update location", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) deviceStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract deviceId from the URL path
	deviceID := strings.TrimPrefix(r.URL.Path, "/devices/")
	deviceID = strings.TrimSuffix(deviceID, "/status")

	if deviceID == "" {
		http.Error(w, "Device ID is required", http.StatusBadRequest)
		return
	}

	// Check if device is registered in the database
	_, err := s.db.GetDeviceByID(deviceID)
	if err != nil {
		// If no document found, it means device is not registered
		if err == mongo.ErrNoDocuments {
			sendJSONResponse(w, http.StatusOK, DeviceStatusResponse{
				IsRegistered: false,
			})
			return
		}

		// Other database errors
		log.Printf("Error checking device status: %v", err)
		http.Error(w, "Failed to check device status", http.StatusInternalServerError)
		return
	}

	// Device exists, so it's registered
	sendJSONResponse(w, http.StatusOK, DeviceStatusResponse{
		IsRegistered: true,
	})
}

// Helper function to send JSON responses
func sendJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
