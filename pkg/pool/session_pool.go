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

type Pool struct {
	Connections map[*PoolConnection]struct{}

	// the subject is the full datasource path ds.{ds}
	URL          string
	Subject      string
	NATSConn     *nats.Conn
	Subscription *nats.Subscription

	Synchronous sync.RWMutex
}

// TODO not a very efficient method, O(N)
func (p *Pool) GetRandomConnection() (*PoolConnection, error) {
	p.Synchronous.RLock()
	defer p.Synchronous.RUnlock()

	if len(p.Connections) == 0 {
		return nil, fmt.Errorf("no connections available in the pool")
	}

	// Convert the map to a slice of its keys
	conns := make([]*PoolConnection, 0, len(p.Connections))
	for conn := range p.Connections {
		conns = append(conns, conn)
	}

	// Select a random connection
	return conns[rand.Intn(len(conns))], nil
}

//// DataReceivedHandler data inbound on NATS subject handled by the implementation of this method.
//// Example: a processor sends streaming data to a NATS subject, data is received on this method and fans out to all entity.go connections
//// Example: a processor sends query request to search a connected datasource, data is received on this method and a datasource connection is selected from the list of entity.go connections
//func (s *Pool) DataReceivedHandler(msg *nats.Msg) {
//	fmt.Println("handling NATS response in base NATSProxy")
//}

func (p *Pool) StreamDataReceivedHandler(m *nats.Msg) {
	// write data to all sockets
	p.Synchronous.RLock()
	var toClose []*PoolConnection
	for wsConn, _ := range p.Connections {
		if err := wsConn.ws.WriteMessage(websocket.TextMessage, m.Data); err != nil {
			log.Printf("failed to write message to remote address: %v, pool: %v, error: %v", wsConn.ws.RemoteAddr(), p.Subject, err)
			toClose = append(toClose, wsConn)
		}
	}
	p.Synchronous.RUnlock()

	// delete connections that failed write
	p.RemoveConnections(toClose)
}

func NewSessionPool(natsURL string, subject string) (*Pool, error) {
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

type PoolConnection struct {
	ws       *websocket.Conn
	Response chan []byte
	mu       sync.RWMutex

	// track the request id with the sent resource operation, this is later used to reply back to the original request forwarded from NATS
	requests map[string]*model.ResourceRequest
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

func (pc *PoolConnection) ResourceReply(operation model.ResourceReply) error {
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
