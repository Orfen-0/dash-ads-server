package mqttclient

import (
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"log"
	"strings"
	"time"

	"github.com/Orfen-0/dash-ads-server/internal/database"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MQTTClient holds our Paho MQTT client plus references we need to handle messages.
type MQTTClient struct {
	client MQTT.Client
	db     *database.MongoDB
}

// NewMQTTClient initializes a Paho MQTT client, connects it to the broker,
// and returns the wrapped MQTTClient struct.
func NewMQTTClient(cfg *config.MQTTConfig, db *database.MongoDB) (*MQTTClient, error) {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(cfg.URI)
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectionLostHandler(func(_ MQTT.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})

	client := MQTT.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	log.Printf("Connected to MQTT broker at %s", cfg.URI)

	mc := &MQTTClient{client: client, db: db}

	// Now subscribe to relevant topics
	mc.subscribeToLocationUpdates()
	mc.subscribeToAcks()
	mc.subscribeToLogs()

	return mc, nil
}

// subscribeToLocationUpdates listens for location messages, e.g. "devices/<deviceID>/location".
func (mc *MQTTClient) subscribeToLocationUpdates() {
	topic := "devices/+/location"
	token := mc.client.Subscribe(topic, 0, mc.handleLocationMessage)
	log.Printf("[MQTT] Subscribed to topic: %s", topic)

	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to location updates: %v", token.Error())
	} else {
		log.Printf("Subscribed to location updates on topic: %s", topic)
	}
}

func (mc *MQTTClient) subscribeToLogs() {
	topic := "devices/+/logs"
	token := mc.client.Subscribe(topic, 0, mc.handleLogMessage)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to log messages: %v", token.Error())
	} else {
		log.Printf("Subscribed to log messages on topic: %s", topic)
	}
}

func (mc *MQTTClient) handleLogMessage(client MQTT.Client, msg MQTT.Message) {
	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		log.Printf("Invalid log topic: %s", msg.Topic())
		return
	}
	deviceID := topicParts[1]

	var payload struct {
		Level     string `json:"level"`
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
		Tag       string `json:"tag"`
	}

	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("Invalid log JSON from device %s: %v", deviceID, err)
		return
	}

	timestamp := time.UnixMilli(payload.Timestamp).Format("2006-01-02 15:04:05.000")
	log.Printf("[AndroidLog] [%s] [%s] [%s] %s - %s", timestamp, deviceID, payload.Tag, strings.ToUpper(payload.Level), payload.Message)

}

// subscribeToAcks listens for ack messages, e.g. "devices/<deviceID>/acks".
func (mc *MQTTClient) subscribeToAcks() {
	topic := "devices/+/acks"
	token := mc.client.Subscribe(topic, 0, mc.handleAckMessage)
	log.Printf("[MQTT] Subscribed to topic: %s", topic)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to ACKs: %v", token.Error())
	} else {
		log.Printf("Subscribed to ack messages on topic: %s", topic)
	}
}

func (mc *MQTTClient) PublishStartStream(deviceID string, eventID primitive.ObjectID) error {
	topic := fmt.Sprintf("devices/%s/cmd", deviceID)

	// A simple JSON payload
	payload := map[string]string{
		"command": "startStream",
		"eventId": eventID.Hex(),
	}
	data, _ := json.Marshal(payload)
	log.Printf("[MQTT] Sending startStream command to device %s for event %s", deviceID, eventID.Hex())

	token := mc.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

func (mc *MQTTClient) PublishStopStream(deviceID string) error {
	topic := fmt.Sprintf("devices/%s/cmd", deviceID)

	// A simple JSON payload
	payload := map[string]string{
		"command": "stopStreaming",
	}
	data, _ := json.Marshal(payload)
	log.Printf("[MQTT] Sending stopStreaming command to device %s", deviceID)

	token := mc.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

// handleLocationMessage is the callback for "devices/+/location" messages.
func (mc *MQTTClient) handleLocationMessage(client MQTT.Client, msg MQTT.Message) {
	// The topic might look like: "devices/myDevice123/location"
	log.Printf("locaiton message received %s", msg.Topic())

	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		log.Printf("Invalid location topic: %s", msg.Topic())
		return
	}
	deviceID := topicParts[1]

	// Parse JSON location data
	var locPayload struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Accuracy  float32 `json:"accuracy"`
		Timestamp int64   `json:"timestamp"`
	}
	log.Printf("[MQTT] Received location update from %s: lat=%.6f lng=%.6f acc=%.1f", deviceID, locPayload.Latitude, locPayload.Longitude, locPayload.Accuracy)

	if err := json.Unmarshal(msg.Payload(), &locPayload); err != nil {
		log.Printf("Invalid location JSON from %s: %v", deviceID, err)
		return
	}

	// Update DB: location
	location := database.DeviceLocation{
		Longitude: locPayload.Longitude,
		Latitude:  locPayload.Latitude,
		Accuracy:  locPayload.Accuracy,
		Timestamp: locPayload.Timestamp,
	}

	err := mc.db.UpdateDeviceLocation(deviceID, location)
	if err != nil {
		log.Printf("Failed to update device location for %s: %v", deviceID, err)
	} else {
		log.Printf("Updated location of device %s to (%.6f,%.6f)", deviceID, locPayload.Latitude, locPayload.Longitude)
	}
}

// handleAckMessage is the callback for "devices/+/acks" messages.
func (mc *MQTTClient) handleAckMessage(client MQTT.Client, msg MQTT.Message) {
	// The topic might look like: "devices/myDevice123/acks"
	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		log.Printf("Invalid ack topic: %s", msg.Topic())
		return
	}
	deviceID := topicParts[1]

	// Example ack payload
	var ack struct {
		Command string `json:"command"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
		log.Printf("Invalid ack JSON from %s: %v", deviceID, err)
		return
	}

	log.Printf("[MQTT] Received ACK from device %s: command=%s, status=%s", deviceID, ack.Command, ack.Status)
	if ack.Command == "startStream" && ack.Status == "ok" {
		log.Printf("[MQTT] Device %s successfully acknowledged stream start", deviceID)
	}
}
