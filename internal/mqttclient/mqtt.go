package mqttclient

import (
	"encoding/json"
	"fmt"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/logging"
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

var (
	logMQTT   = logging.New("mqtt")
	logDevice = logging.New("device") // More granular if needed
)

// NewMQTTClient initializes a Paho MQTT client, connects it to the broker,
// and returns the wrapped MQTTClient struct.
func NewMQTTClient(cfg *config.MQTTConfig, db *database.MongoDB) (*MQTTClient, error) {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(cfg.URI)
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectionLostHandler(func(_ MQTT.Client, err error) {
		logMQTT.Warn("MQTT connection lost: %v", err)
	})

	client := MQTT.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		logMQTT.Error("failed to connect to MQTT broker", "err", err)
		return nil, err
	}

	logMQTT.Info("Connected to MQTT broker at %s", cfg.URI)

	mc := &MQTTClient{client: client, db: db}

	mc.subscribeToLocationUpdates()
	mc.subscribeToAcks()
	mc.subscribeToLogs()

	return mc, nil
}

// subscribeToLocationUpdates listens for location messages, e.g. "devices/<deviceID>/location".
func (mc *MQTTClient) subscribeToLocationUpdates() {
	topic := "devices/+/location"
	token := mc.client.Subscribe(topic, 0, mc.handleLocationMessage)

	token.Wait()
	if token.Error() != nil {
		logMQTT.Error("Failed to subscribe to location updates: %v", token.Error())
	} else {
		logMQTT.Info("Subscribed to location updates on topic", "topic", topic)
	}
}

func (mc *MQTTClient) subscribeToLogs() {
	topic := "devices/+/logs"
	token := mc.client.Subscribe(topic, 0, mc.handleLogMessage)
	token.Wait()
	if token.Error() != nil {
		logMQTT.Error("Failed to subscribe to log messages: %v", token.Error())
	} else {
		logMQTT.Info("Subscribed to log messages on topic: %s", topic)
	}
}

func (mc *MQTTClient) handleLogMessage(client MQTT.Client, msg MQTT.Message) {
	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		logMQTT.Warn("Invalid log topic", "topic", msg.Topic())
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
		logMQTT.Error("Invalid log JSON from device %s: %v", deviceID, err)
		return
	}

	logDevice.Info(
		payload.Message,
		"device_id", deviceID,
		"tag", payload.Tag,
		"level", strings.ToUpper(payload.Level),
		"device_ts", time.UnixMilli(payload.Timestamp).Format("2006-01-02 15:04:05.000"),
	)

}

// subscribeToAcks listens for ack messages, e.g. "devices/<deviceID>/acks".
func (mc *MQTTClient) subscribeToAcks() {
	topic := "devices/+/acks"
	token := mc.client.Subscribe(topic, 0, mc.handleAckMessage)
	token.Wait()
	if token.Error() != nil {
		logMQTT.Error("Failed to subscribe to ACKs: %v", token.Error())
	} else {
		logMQTT.Info("Subscribed to ack messages", "topic", topic)
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
	logMQTT.Info("Sending startStream command to device for event", "deviceId", deviceID, "eventId", eventID.Hex())

	token := mc.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

func (mc *MQTTClient) PublishStopStream(deviceID string) error {
	topic := fmt.Sprintf("devices/%s/cmd", deviceID)

	payload := map[string]string{
		"command": "stopStreaming",
	}
	data, _ := json.Marshal(payload)
	logMQTT.Info("Sending stopStreaming command to device", "deviceId", deviceID)

	token := mc.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

// handleLocationMessage is the callback for "devices/+/location" messages.
func (mc *MQTTClient) handleLocationMessage(client MQTT.Client, msg MQTT.Message) {

	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		logMQTT.Error("Invalid location topic", "topic", msg.Topic())
		return
	}
	deviceID := topicParts[1]
	logMQTT.Info("location message received", "deviceId", deviceID, "topic", msg.Topic())

	// Parse JSON location data
	var locPayload struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Accuracy  float32 `json:"accuracy"`
		Timestamp int64   `json:"timestamp"`
	}
	logMQTT.Info("Received location update, lat=%.6f lng=%.6f acc=%.1f", locPayload.Latitude, locPayload.Longitude, locPayload.Accuracy, "deviceId", deviceID)

	if err := json.Unmarshal(msg.Payload(), &locPayload); err != nil {
		logMQTT.Error("Invalid location JSON", "deviceId", deviceID, "err", err)
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
		logMQTT.Error("Failed to update device location", "deviceId", deviceID, "err", err)
	} else {
		logMQTT.Info("Updated location of device to (%.6f,%.6f)", locPayload.Latitude, locPayload.Longitude, "deviceId", deviceID)
	}
}

// handleAckMessage is the callback for "devices/+/acks" messages.
func (mc *MQTTClient) handleAckMessage(client MQTT.Client, msg MQTT.Message) {
	topicParts := strings.Split(msg.Topic(), "/")
	if len(topicParts) < 3 {
		logMQTT.Error("Invalid ack topic", "topic", msg.Topic())
		return
	}
	deviceID := topicParts[1]

	// Example ack payload
	var ack struct {
		Command string `json:"command"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
		logMQTT.Error("Invalid ack JSON", "deviceId", deviceID, "err", err)
		return
	}

	logMQTT.Info("Received ACK from device", "deviceId", deviceID, "command", ack.Command, "status", ack.Status)
	if ack.Command == "startStream" && ack.Status == "ok" {
		logMQTT.Info("Device successfully acknowledged stream start", "deviceId", deviceID)
	}
}
