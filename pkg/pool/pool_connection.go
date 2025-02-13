package pool

import (
	"alethic-ism-stream-api/pkg/model"
	"github.com/gorilla/websocket"
	"sync"
)

type PoolConnection struct {
	ws       *websocket.Conn
	Response chan []byte
	mu       sync.RWMutex

	// TODO need to ensure we clean up dead requests

	// track the request for reply , this is later used to reply back to the original request forwarded from NATS
	requests map[string]*model.ResourceRequest
}
