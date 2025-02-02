package database

import (
	"context"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDB struct {
	client  *mongo.Client
	db      *mongo.Database
	devices *mongo.Collection
	events  *mongo.Collection
	streams *mongo.Collection
}

type GeoJSONPoint struct {
	Type        string     `bson:"type"`        // "Point"
	Coordinates [2]float64 `bson:"coordinates"` // [lng, lat]
}

type Stream struct {
	ID            primitive.ObjectID  `bson:"_id,omitempty"`
	EventID       *primitive.ObjectID `bson:"eventId,omitempty"`
	DeviceId      string              `bson:"deviceId"`
	Title         string              `bson:"title"`
	Description   string              `bson:"description"`
	StartTime     time.Time           `bson:"startTime"`
	EndTime       *time.Time          `bson:"endTime,omitempty"`
	Status        string              `bson:"status"`
	RTMPURL       string              `bson:"rtmpUrl"`
	PlaybackURL   string              `bson:"playbackUrl"`
	StartLocation GeoJSONPoint        `bson:"startLocation"`
	LocAccuracy   float32             `bson:"locAccuracy"`
}

type Device struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	DeviceID      string             `bson:"deviceId"`
	Model         string             `bson:"model"`
	Manufacturer  string             `bson:"manufacturer"`
	OsVersion     string             `bson:"osVersion"`
	LastLocation  GeoJSONPoint       `bson:"lastLocation"`
	LocAccuracy   float32            `bson:"locAccuracy"`
	LocUpdate     time.Time          `bson:"locUpdate"`
	RegisteredAt  time.Time          `bson:"registeredAt"`
	LastUpdatedAt time.Time          `bson:"lastUpdatedAt"`
}

type DeviceLocation struct {
	Latitude  float64 `bson:"latitude"`
	Longitude float64 `bson:"longitude"`
	Accuracy  float32 `bson:"accuracy"`
	Timestamp int64   `bson:"timestamp"`
}

type Event struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	TriggeredBy string             `bson:"triggeredBy"` // e.g. which user/device triggered it
	StartTime   time.Time          `bson:"startTime"`
	EndTime     *time.Time         `bson:"endTime,omitempty"`
	Location    GeoJSONPoint       `bson:"location"`
	Radius      float64            `bson:"radius"`
	Status      string             `bson:"status"` // e.g. "active", "ended"
}

type EventLocation struct {
	Latitude  float64 `bson:"latitude"`
	Longitude float64 `bson:"longitude"`
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
		events:  db.Collection("events"),
		streams: db.Collection("streams"),
	}, nil
}

func (m *MongoDB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.client.Disconnect(ctx)
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

func (m *MongoDB) CreateEvent(event *Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.events.InsertOne(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}
	return nil
}

func (m *MongoDB) GetEvent(id primitive.ObjectID) (*Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var evt Event
	err := m.events.FindOne(ctx, bson.M{"_id": id}).Decode(&evt)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("event not found")
		}
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	return &evt, nil
}

func (m *MongoDB) UpdateEventStatus(id primitive.ObjectID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.events.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"status": status}},
	)
	if err != nil {
		return fmt.Errorf("failed to update event status: %w", err)
	}
	return nil
}

func (m *MongoDB) ListEventsByDateRange(from, to time.Time) ([]*Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build a filter
	filter := bson.M{}
	// If 'from' is non-zero, { "startTime": { $gte: from } }
	if !from.IsZero() {
		filter["startTime"] = bson.M{"$gte": from}
	}
	// If 'to' is non-zero, add $lte to the same field
	if !to.IsZero() {
		// If filter["startTime"] doesn't exist yet, define it
		if _, ok := filter["startTime"]; !ok {
			filter["startTime"] = bson.M{}
		}
		filter["startTime"].(bson.M)["$lte"] = to
	}

	cursor, err := m.events.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*Event
	for cursor.Next(ctx) {
		var e Event
		if err := cursor.Decode(&e); err != nil {
			log.Printf("Error decoding event doc: %v", err)
			continue
		}
		results = append(results, &e)
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}
	return results, nil
}

func (m *MongoDB) UpdateDeviceLocation(deviceID string, loc DeviceLocation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Convert timestamp (milliseconds) to time.Time
	updateTime := time.Unix(
		loc.Timestamp/1000,
		(loc.Timestamp%1000)*int64(time.Millisecond),
	)

	// Construct GeoJSON point
	geoPoint := GeoJSONPoint{
		Type:        "Point",
		Coordinates: [2]float64{loc.Longitude, loc.Latitude}, // [lng, lat]
	}

	collection := m.db.Collection("devices")
	result, err := collection.UpdateOne(
		ctx,
		bson.M{"deviceId": deviceID},
		bson.M{
			"$set": bson.M{
				"lastLocation":  geoPoint,
				"locAccuracy":   loc.Accuracy,
				"locUpdate":     updateTime,
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

func (m *MongoDB) FindDevicesInRadius(lng, lat float64, radiusKM float64) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // increased timeout
	defer cancel()

	radiusMeters := radiusKM * 1000

	// Build the geo query filter.
	filter := bson.M{
		"lastLocation": bson.M{
			"$near": bson.M{
				"$geometry": bson.M{
					"type":        "Point",
					"coordinates": []float64{lng, lat}, // [lng, lat]
				},
				"$maxDistance": radiusMeters,
			},
		},
	}

	// Log the filter for debugging.
	log.Printf("Geo query filter: %+v", filter)

	// Project only the deviceId field.
	projection := bson.M{"deviceId": 1}

	collection := m.db.Collection("devices")
	cursor, err := collection.Find(ctx, filter, options.Find().SetProjection(projection))
	if err != nil {
		log.Printf("Error executing Find: %v", err)
		return nil, fmt.Errorf("failed to find devices in radius: %w", err)
	}
	defer cursor.Close(ctx)

	var result []string
	for cursor.Next(ctx) {
		var doc struct {
			DeviceID string `bson:"deviceId"`
		}
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("Error decoding document: %v", err)
			continue
		}
		result = append(result, doc.DeviceID)
	}
	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	log.Printf("Found %d devices", len(result))
	return result, nil
}

func (m *MongoDB) CreateStream(stream *Stream) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.streams.InsertOne(ctx, stream)
	if err != nil {
		return fmt.Errorf("failed to insert stream: %w", err)
	}

	return nil
}

func (m *MongoDB) GetStream(id primitive.ObjectID) (*Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stream Stream
	err := m.streams.FindOne(ctx, bson.M{"_id": id}).Decode(&stream)
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

	_, err := m.streams.UpdateOne(
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

	_, err := m.streams.UpdateOne(
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

	cursor, err := m.streams.Find(ctx, bson.M{"status": "live"})
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
