package rtmp

import (
	"sync"

	"github.com/nareix/joy4/format/rtmp"
)

type LiveStreamManager struct {
	streams map[string]*LiveStream
	mu      sync.Mutex
}

type LiveStream struct {
	Publisher *rtmp.Conn
	Clients   []*rtmp.Conn
}

func NewLiveStreamManager() *LiveStreamManager {
	return &LiveStreamManager{
		streams: make(map[string]*LiveStream),
	}
}

func (m *LiveStreamManager) AddPublisher(streamID string, conn *rtmp.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[streamID] = &LiveStream{Publisher: conn, Clients: []*rtmp.Conn{}}
}

func (m *LiveStreamManager) AddClient(streamID string, conn *rtmp.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stream, ok := m.streams[streamID]; ok {
		stream.Clients = append(stream.Clients, conn)
	}
}

func (m *LiveStreamManager) RemoveClient(streamID string, conn *rtmp.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stream, ok := m.streams[streamID]; ok {
		for i, client := range stream.Clients {
			if client == conn {
				stream.Clients = append(stream.Clients[:i], stream.Clients[i+1:]...)
				break
			}
		}
	}
}

func (m *LiveStreamManager) RemovePublisher(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, streamID)
}

func (m *LiveStreamManager) GetPublisher(streamID string) *rtmp.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stream, ok := m.streams[streamID]; ok {
		return stream.Publisher
	}
	return nil
}

func (m *LiveStreamManager) GetClients(streamID string) []*rtmp.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stream, ok := m.streams[streamID]; ok {
		return stream.Clients
	}
	return nil
}
