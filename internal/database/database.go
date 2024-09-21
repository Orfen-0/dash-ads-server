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
	client *mongo.Client
	db     *mongo.Database
}

type Stream struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	UserID      primitive.ObjectID `bson:"userId"`
	Title       string             `bson:"title"`
	Description string             `bson:"description"`
	StartTime   time.Time          `bson:"startTime"`
	EndTime     *time.Time         `bson:"endTime,omitempty"`
	Status      string             `bson:"status"`
	RTMPURL     string             `bson:"rtmpUrl"`
	PlaybackURL string             `bson:"playbackUrl"`
}

func NewMongoDB(cfg *config.MongoDBConfig) (*MongoDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return &MongoDB{
		client: client,
		db:     client.Database(cfg.Database),
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
