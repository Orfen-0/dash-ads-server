package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Orfen-0/dash-ads-server/internal/config"
)

type MongoDB struct {
	client  *mongo.Client
	db      *mongo.Database
	devices *mongo.Collection
}

type Stream struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	DeviceId    string             `bson:"deviceId"`
	Title       string             `bson:"title"`
	Description string             `bson:"description"`
	StartTime   time.Time          `bson:"startTime"`
	EndTime     *time.Time         `bson:"endTime,omitempty"`
	Status      string             `bson:"status"`
	RTMPURL     string             `bson:"rtmpUrl"`
	PlaybackURL string             `bson:"playbackUrl"`
	Latitude    string             `bson:"latitude"`
	Longitude   string             `bson:"longitude"`
	LocAccuracy string             `bson:"locAccuracy"`
}

type Device struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	DeviceID      string             `bson:"deviceId"`
	Model         string             `bson:"model"`
	Manufacturer  string             `bson:"manufacturer"`
	OsVersion     string             `bson:"osVersion"`
	LastLocation  DeviceLocation     `bson:"lastLocation"`
	RegisteredAt  time.Time          `bson:"registeredAt"`
	LastUpdatedAt time.Time          `bson:"lastUpdatedAt"`
}

type DeviceLocation struct {
	Latitude  float64 `bson:"latitude"`
	Longitude float64 `bson:"longitude"`
	Accuracy  float32 `bson:"accuracy"`
	Timestamp int64   `bson:"timestamp"`
}

func (m *MongoDB) RegisterDevice(device *Device) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("devices")

	var existingDevice Device
	err := collection.FindOne(ctx, bson.M{"deviceId": device.DeviceID}).Decode(&existingDevice)

	if err == nil {
		_, err = collection.UpdateOne(
			ctx,
			bson.M{"deviceId": device.DeviceID},
			bson.M{
				"$set": bson.M{
					"model":         device.Model,
					"manufacturer":  device.Manufacturer,
					"osVersion":     device.OsVersion,
					"lastLocation":  device.LastLocation,
					"lastUpdatedAt": time.Now(),
				},
			},
		)
		return err
	} else if err == mongo.ErrNoDocuments {
		device.RegisteredAt = time.Now()
		device.LastUpdatedAt = time.Now()
		_, err = collection.InsertOne(ctx, device)
		return err
	}

	return err
}

func (m *MongoDB) UpdateDeviceLocation(deviceID string, location DeviceLocation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("devices")
	result, err := collection.UpdateOne(
		ctx,
		bson.M{"deviceId": deviceID},
		bson.M{
			"$set": bson.M{
				"lastLocation":  location,
				"lastUpdatedAt": time.Now(),
			},
		},
	)

	if err != nil {
		return fmt.Errorf("failed to update device location: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("device not found")
	}

	return nil
}

func (m *MongoDB) GetDevice(deviceID string) (*Device, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("devices")
	var device Device
	err := collection.FindOne(ctx, bson.M{"deviceId": deviceID}).Decode(&device)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("device not found")
		}
		return nil, fmt.Errorf("failed to get device: %w", err)
	}

	return &device, nil
}

func NewMongoDB(cfg *config.MongoDBConfig) (*MongoDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Printf("Connecting to MongoDB with URI: %s\n", cfg.URI)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}
	fmt.Println("Successfully connected to MongoDB")

	db := client.Database(cfg.Database)
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}
	fmt.Printf("Available collections: %v\n", collections)

	return &MongoDB{
		client:  client,
		db:      db,
		devices: db.Collection("devices"),
	}, nil
}

func (m *MongoDB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.client.Disconnect(ctx)
}

func (m *MongoDB) CreateStream(stream *Stream) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("streams")
	_, err := collection.InsertOne(ctx, stream)
	if err != nil {
		return fmt.Errorf("failed to insert stream: %w", err)
	}

	return nil
}

func (m *MongoDB) GetStream(id primitive.ObjectID) (*Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("streams")
	var stream Stream
	err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&stream)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("stream not found")
		}
		return nil, fmt.Errorf("failed to get stream: %w", err)
	}

	return &stream, nil
}

func (m *MongoDB) UpdateStreamStatus(id primitive.ObjectID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("streams")
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"status": status}},
	)
	if err != nil {
		return fmt.Errorf("failed to update stream status: %w", err)
	}

	return nil
}

func (m *MongoDB) EndStream(id primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := m.db.Collection("streams")
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{
				"status":  "ended",
				"endTime": time.Now(),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to end stream: %w", err)
	}

	return nil
}

func (m *MongoDB) GetDeviceByID(deviceID string) (*Device, error) {
	var device Device
	err := m.devices.FindOne(context.Background(), bson.M{"deviceId": deviceID}).Decode(&device)
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func (m *MongoDB) ListActiveStreams() ([]*Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := m.db.Collection("streams")
	cursor, err := collection.Find(ctx, bson.M{"status": "live"})
	if err != nil {
		return nil, fmt.Errorf("failed to list active streams: %w", err)
	}
	defer cursor.Close(ctx)

	var streams []*Stream
	for cursor.Next(ctx) {
		var stream Stream
		if err := cursor.Decode(&stream); err != nil {
			log.Printf("Error decoding stream: %v", err)
			continue
		}
		streams = append(streams, &stream)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return streams, nil
}
