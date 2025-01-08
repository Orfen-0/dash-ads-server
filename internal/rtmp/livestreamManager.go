package rtmp

import (
	"sync"

	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/format/rtmp"
)

// LiveStream represents a publisher plus multiple clients.
type LiveStream struct {
	Publisher *rtmp.Conn
	Streams   []av.CodecData // Store the publisher's codec data
	Clients   map[*ClientConn]bool
}

type LiveStreamManager struct {
	mu      sync.Mutex
	streams map[string]*LiveStream
}

func NewLiveStreamManager() *LiveStreamManager {
	return &LiveStreamManager{
		streams: make(map[string]*LiveStream),
	}
}

// ClientConn holds a channel for packets and a pointer to the RTMP connection.
type ClientConn struct {
	Conn       *rtmp.Conn
	PacketChan chan av.Packet
}

func (m *LiveStreamManager) AddPublisher(streamID string, publisher *rtmp.Conn, codecData []av.CodecData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[streamID] = &LiveStream{
		Publisher: publisher,
		Streams:   codecData,
		Clients:   make(map[*ClientConn]bool),
	}
}

func (m *LiveStreamManager) RemovePublisher(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, streamID)
}

func (m *LiveStreamManager) GetPublisher(streamID string) *LiveStream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[streamID]
}

func (m *LiveStreamManager) AddClient(streamID string, c *ClientConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ls, ok := m.streams[streamID]; ok {
		ls.Clients[c] = true
	}
}

func (m *LiveStreamManager) RemoveClient(streamID string, c *ClientConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ls, ok := m.streams[streamID]; ok {
		delete(ls.Clients, c)
	}
}

func (m *LiveStreamManager) ForwardPacket(streamID string, packet av.Packet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ls, ok := m.streams[streamID]
	if !ok {
		return
	}
	for client := range ls.Clients {
		select {
		case client.PacketChan <- packet:
			// Sent successfully
		default:
			// Client channel is full or slow; decide how to handle
		}
	}
}

func (m *LiveStreamManager) CloseAllClients(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ls, ok := m.streams[streamID]
	if !ok {
		return
	}

	// Close each client's channel or RTMP connection
	for client := range ls.Clients {
		close(client.PacketChan)
		// Optionally client.Conn.Close() if we want to forcefully close the RTMP connection
		delete(ls.Clients, client)
	}
}
