package http

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/rtmp"
	"log"
	"net/http"
	"time"
)

type Server struct {
	config     *config.HTTPConfig
	httpServer *http.Server
	rtmpServer *rtmp.Server
}

type Config struct {
	Port string
}

func NewServer(config *config.HTTPConfig, rtmpServer *rtmp.Server) *Server {
	return &Server{
		config:     config,
		rtmpServer: rtmpServer,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Apply middleware
	handler := s.loggingMiddleware(s.corsMiddleware(mux))

	// Set up routes
	mux.HandleFunc("/health", s.healthCheckHandler)
	mux.HandleFunc("/streams", s.streamsHandler)

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

// Helper function to send JSON responses
func sendJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
