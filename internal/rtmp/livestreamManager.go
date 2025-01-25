package rtmp

import (
	"log"
	"sync"

	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/format/rtmp"
)

type LiveStream struct {
	Publisher   *rtmp.Conn
	Streams     []av.CodecData
	RTMPClients map[*rtmp.Conn]bool
	HTTPClients map[chan av.Packet]bool
	mu          sync.RWMutex
}
type LiveStreamManager struct {
	mu      sync.RWMutex
	streams map[string]*LiveStream
}

func NewLiveStreamManager() *LiveStreamManager {
	return &LiveStreamManager{
		streams: make(map[string]*LiveStream),
	}
}

func (m *LiveStreamManager) AddPublisher(streamID string, conn *rtmp.Conn, streams []av.CodecData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[streamID] = &LiveStream{
		Publisher:   conn,
		Streams:     streams,
		RTMPClients: make(map[*rtmp.Conn]bool),
		HTTPClients: make(map[chan av.Packet]bool),
	}
}

func (m *LiveStreamManager) RemovePublisher(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, streamID)
}

func (m *LiveStreamManager) AddRTMPClient(streamID string, client *rtmp.Conn) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.Lock()
		stream.RTMPClients[client] = true
		stream.mu.Unlock()
	}
}

func (m *LiveStreamManager) RemoveRTMPClient(streamID string, client *rtmp.Conn) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.Lock()
		delete(stream.RTMPClients, client)
		stream.mu.Unlock()
	}
}

func (m *LiveStreamManager) AddHTTPClient(streamID string, clientChan chan av.Packet) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.Lock()
		stream.HTTPClients[clientChan] = true
		stream.mu.Unlock()
	}
}

func (m *LiveStreamManager) RemoveHTTPClient(streamID string, clientChan chan av.Packet) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.Lock()
		delete(stream.HTTPClients, clientChan)
		close(clientChan)
		stream.mu.Unlock()
	}
}

func (m *LiveStreamManager) ForwardPacket(streamID string, packet av.Packet) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.RLock()
		// Forward to RTMP clients
		for client := range stream.RTMPClients {
			if err := client.WritePacket(packet); err != nil {
				log.Printf("Failed to write packet to RTMP client: %v", err)
			}
		}
		// Forward to HTTP-FLV clients
		for clientChan := range stream.HTTPClients {
			select {
			case clientChan <- packet:
			default:
				log.Printf("HTTP-FLV client channel full, dropping packet")
			}
		}
		stream.mu.RUnlock()
	}
}
func (m *LiveStreamManager) GetPublisher(streamID string) *LiveStream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streams[streamID]
}

func (m *LiveStreamManager) CloseAllClients(streamID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.mu.Lock()
		// Close HTTP-FLV client channels
		for clientChan := range stream.HTTPClients {
			close(clientChan)
			delete(stream.HTTPClients, clientChan)
		}
		// Close RTMP clients
		for client := range stream.RTMPClients {
			client.Close()
			delete(stream.RTMPClients, client)
		}
		stream.mu.Unlock()
	}
}
