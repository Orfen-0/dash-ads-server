package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type RTMPConfig struct {
	Port   string `json:"port"`
	Domain string `json:"domain"` // For generating URLs
}
type Config struct {
	RTMP    RTMPConfig
	HTTP    HTTPConfig
	Storage MinIOConfig
	MongoDB MongoDBConfig
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

func Load() (*Config, error) {
	err := godotenv.Load("env.local")
	if err != nil {
		fmt.Println("Warning: Error loading .env file")
	}

	config := &Config{
		RTMP: RTMPConfig{
			Port: getEnv("RTMP_PORT", "1935"),
		},
		HTTP: HTTPConfig{
			Port: getEnv("HTTP_PORT", "8080"),
		},
		Storage: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			UseSSL:    getEnvAsBool("MINIO_USE_SSL", false),
			Bucket:    getEnv("MINIO_BUCKET", "videos"),
		},
		MongoDB: MongoDBConfig{
			URI:      getEnv("MONGODB_URI", "mongodb://root:example@localhost:27017"),
			Database: getEnv("MONGODB_DATABASE", "dash_ads_server"),
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
