package config

import (
	"github.com/Orfen-0/dash-ads-server/internal/logging"
	"net"
	"os"

	"github.com/joho/godotenv"
)

type RTMPConfig struct {
	Port     string `json:"port"`
	Domain   string `json:"domain"` // For generating URLs
	HTTPPort string `json:"http-port"`
}
type Config struct {
	RTMP    RTMPConfig
	HTTP    HTTPConfig
	Storage MinIOConfig
	MongoDB MongoDBConfig
	MQTT    MQTTConfig
}

type HTTPConfig struct {
	Port string
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

type MongoDBConfig struct {
	URI      string
	Database string
}

type MQTTConfig struct {
	URI      string
	Username string
	Password string
}

var logger = logging.New("init")

func Load() (*Config, error) {
	// Load .env file
	err := godotenv.Load(".env.local")
	if err != nil {
		logger.Error("Warning: Error loading .env file")
	}

	config := &Config{
		RTMP: RTMPConfig{
			Port:     getEnv("RTMP_PORT", "1935"),
			Domain:   getEnv("RTMP_DOMAIN", getFallbackDomain()),
			HTTPPort: getEnv("HTTP_PORT", "8080"),
		},
		HTTP: HTTPConfig{
			Port: getEnv("HTTP_PORT", "8080"),
		},
		Storage: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
			UseSSL:    getEnvAsBool("MINIO_USE_SSL", false),
			Bucket:    getEnv("MINIO_BUCKET", "streams"),
		},
		MongoDB: MongoDBConfig{
			URI:      getEnv("MONGODB_URI", "mongodb://root:example@localhost:27017"),
			Database: getEnv("MONGODB_DATABASE", "dash_ads_server"),
		},
		MQTT: MQTTConfig{
			URI:      getEnv("MQTT_URI", "mqtt://localhost:1883"),
			Username: getEnv("MQTT_USERNAME", ""),
			Password: getEnv("MQTT_PASSWORD", ""),
		},
	}

	return config, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		return value == "true" || value == "1"
	}
	return fallback
}

func getFallbackDomain() string {
	// Try to determine the local IP address
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Warn("Error getting local IP:", err)
		return "localhost" // Fallback to localhost
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}

	return "localhost" // Final fallback if no suitable IP is found
}
