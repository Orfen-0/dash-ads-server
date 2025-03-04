package mqttclient

import (
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"log"
	"strings"

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

	return mc, nil
}

// subscribeToLocationUpdates listens for location messages, e.g. "devices/<deviceID>/location".
func (mc *MQTTClient) subscribeToLocationUpdates() {
	topic := "devices/+/location"
	token := mc.client.Subscribe(topic, 0, mc.handleLocationMessage)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to location updates: %v", token.Error())
	} else {
		log.Printf("Subscribed to location updates on topic: %s", topic)
	}
}

// subscribeToAcks listens for ack messages, e.g. "devices/<deviceID>/acks".
func (mc *MQTTClient) subscribeToAcks() {
	topic := "devices/+/acks"
	token := mc.client.Subscribe(topic, 0, mc.handleAckMessage)
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
	log.Printf("forwarding command to % command topic", deviceID)

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
	log.Printf("forwarding command to % command topic", deviceID)

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

	log.Printf("ACK from device %s: command=%s, status=%s", deviceID, ack.Command, ack.Status)

	// e.g. if ack.Command == "startStream" && ack.Status == "ok", we know device is responding
	// Possibly update DB or log an event
	if ack.Command == "startStream" && ack.Status == "ok" {
		// Mark device as streaming, or store last ack time, etc.
		log.Printf("Device %s acknowledged start stream", deviceID)
		// mc.db.SetDeviceStreaming(deviceID, true) // example
	}
}
