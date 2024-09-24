package model

import (
	"encoding/json"
	"github.com/nats-io/nats.go"
)

type ProcessRequest struct {
	ID        string            `json:"id"`
	ProcessID string            `json:"processId"`
	Operation string            `json:"operation"`
	Payload   []json.RawMessage `json:"payload"`

	NATSMessage *nats.Msg `json:"-"`
}

type ProcessReply struct {
	ID      string            `json:"id"`
	Payload []json.RawMessage `json:"payload"`

	RequestID        string `json:"requestId"`
	RequestProcessID string `json:"processId"`
}

type ResourceRequest struct {
	ID        string            `json:"id"`
	Operation string            `json:"operation"`
	Payload   []json.RawMessage `json:"payload"`

	// data that we save for when we want to reply, if required that is not part of the request headers
	ProcessRequest ProcessRequest `json:"-"`
}

type ResourceReply struct {
	ID      string            `json:"id"`
	Payload []json.RawMessage `json:"payload"`

	// original request id
	RequestID string `json:"requestId"`

	// channel to stream the data if any
}
