package pool

import (
	"alethic-ism-stream-api/pkg/model"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"log"
	"math/rand"
	"sync"
	"time"
)

// Pool captures a map of websocket connections, as related to the NATS subject. This allows client connections to
// receive a stream of events, as events are processed off the nats subject queue. A pool also ensures connectivity
// is maintained with the respective NATS subject channel, and manages the reconciliation of dead websockets.
type Pool struct {
	Connections map[*PoolConnection]struct{}

	// the subject is the full datasource path ds.{ds}
	URL          string
	Subject      string
	NATSConn     *nats.Conn
	Subscription *nats.Subscription

	Synchronous sync.RWMutex
}

// GetRandomConnection fetches a random websocket connection from the set of active connections.
// TODO not a very efficient method, O(N), least-accessed might be more effective.
func (s *Pool) GetRandomConnection() (*PoolConnection, error) {
	s.Synchronous.RLock()
	defer s.Synchronous.RUnlock()

	if len(s.Connections) == 0 {
		return nil, fmt.Errorf("no connections available in the pool")
	}

	// Convert the map to a slice of its keys
	connections := make([]*PoolConnection, 0, len(s.Connections))
	for conn := range s.Connections {
		connections = append(connections, conn)
	}

	// Select a random connection
	return connections[rand.Intn(len(connections))], nil
}

func (s *Pool) StreamDataReceivedHandler(m *nats.Msg) {
	// write data to all sockets
	s.Synchronous.RLock()
	var toClose []*PoolConnection
	for wsConn, _ := range s.Connections {
		if err := wsConn.ws.WriteMessage(websocket.TextMessage, m.Data); err != nil {
			log.Printf("failed to write message to remote address: %v, pool: %v, error: %v", wsConn.ws.RemoteAddr(), s.Subject, err)
			toClose = append(toClose, wsConn)
		}
	}
	s.Synchronous.RUnlock()

	// delete connections that failed write
	s.RemoveConnections(toClose)
}

func NewPool(natsURL string, subject string) (*Pool, error) {
	sp := &Pool{
		Connections: make(map[*PoolConnection]struct{}),
		Subject:     subject,
	}

	// Create a new NATS connection
	nc, err := nats.Connect(natsURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("NATS connection disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS connection reconnected")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %v", err)
	}
	sp.NATSConn = nc

	// Wait for the connection to be established
	if !nc.IsConnected() {
		log.Println("Waiting for NATS connection to be established...")
		for i := 0; i < 5; i++ { // Try for 5 seconds
			if nc.IsConnected() {
				break
			}
			time.Sleep(time.Second)
		}
		if !nc.IsConnected() {
			return nil, fmt.Errorf("failed to establish NATS connection after 5 seconds")
		}
	}

	return sp, nil
}

func (s *Pool) Subscribe(subject string, callback nats.MsgHandler) (*nats.Subscription, error) {
	// Set up the NATS subscription
	sub, err := s.NATSConn.Subscribe(subject, callback)
	if err != nil {
		s.NATSConn.Close()
		return nil, fmt.Errorf("failed to subscribe to NATS subject %s: %v", subject, err)
	}
	s.Subscription = sub
	log.Printf("Successfully created SessionPool for subject: %s", subject)

	return sub, nil
}

func (pc *PoolConnection) String() string {
	return pc.ws.RemoteAddr().String()
}

func (pc *PoolConnection) WriteText(payload []byte) error {
	return pc.ws.WriteMessage(websocket.TextMessage, payload)
}

func NewPoolConnection(ws *websocket.Conn) *PoolConnection {
	return &PoolConnection{
		ws:       ws,
		Response: make(chan []byte),
		requests: make(map[string]*model.ResourceRequest),
	}
}

func (pc *PoolConnection) WaitResourceReply(dsPool *Pool, conn *PoolConnection) {
	// Handle incoming messages from the WebSocket
	for {

		/// TODO probably need a graceful exit -- MAYBE use a channel instead

		_, message, err := conn.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var reply model.ResourceReply
		if err := json.Unmarshal(message, &reply); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// check to ensure that the inbound reply from the websocket matches the requests that went out to that websocket
		id := reply.RequestID
		request, exists := pc.requests[id]
		if !exists {
			log.Printf("received message from websocket, with corresponding request id, ignoring message request: %v\n", id)
			// TODO critical log?
			return
		}
		delete(pc.requests, id) // ensure to remove the nats request from the queue, since we got a resource reply from the websocket

		// fetch the original nats request payload, we will use this to reconstruct our reply back to the nats requester
		processRequest := request.ProcessRequest

		// create a reply back to the original nats requester and serialize thereafter
		poReply := model.ProcessReply{
			ID:               reply.ID,
			Payload:          reply.Payload,
			RequestID:        request.ID,
			RequestProcessID: processRequest.ProcessID,
		}

		// serialize the reply and check for validity
		poReplyJSON, err := json.Marshal(poReply)
		if err != nil {
			log.Printf("error marshaling po reply: %v", err)
			// TODO
		}

		// submit the serialized reply (as a json bytes array) back to the originating requester
		// **note: the NATSMessage is the same pointer that we originally received on the NATS message/request handler
		err = processRequest.NATSMessage.Respond(poReplyJSON)
		if err != nil {
			log.Printf("error replying back process request: %v, error: %v", processRequest.ID, err)
			return
		}

		log.Printf("successfully completed request: %v, operation: %s", processRequest.ID, processRequest.Operation)
	}
}

func (pc *PoolConnection) ResourceRequest(processRequest model.ProcessRequest) error {
	request := model.ResourceRequest{
		ID:             uuid.NewString(),
		Operation:      processRequest.Operation,
		Payload:        processRequest.Payload,
		ProcessRequest: processRequest,
	}

	// store the request for later retrieval
	pc.requests[request.ID] = &request

	// serialize the request and submit to websocket connection
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v, error: %v", request.ID, err)
	}

	// write the serialized data directly to the socket.
	err = pc.ws.WriteMessage(websocket.TextMessage, requestJSON)
	if err != nil {
		delete(pc.requests, request.ID)
		return fmt.Errorf("failed to write request: %v, error: %v", request.ID, request)
	}

	return nil
}

func (s *Pool) AddConnection(conn *PoolConnection) error {
	// establish connection lock
	s.Synchronous.Lock()
	defer s.Synchronous.Unlock()
	s.Connections[conn] = struct{}{}
	return nil
}

func (s *Pool) RemoveConnection(conn *PoolConnection) {
	s.RemoveConnections([]*PoolConnection{conn})
}

func (s *Pool) RemoveConnections(connections []*PoolConnection) {
	s.Synchronous.Lock()
	defer s.Synchronous.Unlock()
	for _, conn := range connections {
		err := conn.ws.Close()
		if err != nil {
			log.Printf("failed to close connection remote address: %v, error: %v", conn.ws.RemoteAddr(), err)
		}
		delete(s.Connections, conn)
	}
}

//
//func (s *Pool) Request(data []byte) (*nats.Msg, error) {
//	request, err := s.NATSConn.Request(s.Subject, data, time.Second*10)
//	if err != nil {
//		return nil, err
//	}
//	return request, nil
//}
//
//// Reply sends a reply to a received message
//func (s *Pool) Reply(msg *nats.Msg, data []byte) error {
//	if msg.Reply == "" {
//		return fmt.Errorf("cannot reply: original message has no reply subject")
//	}
//	return s.NATSConn.Publish(msg.Reply, data)
//}
