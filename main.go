package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
//RedisPort = os.Getenv("REDIS_PORT")
//RedisHost = os.Getenv("REDIS_HOST")
//RedisPass = os.Getenv("REDIS_PASS")

// redisClient, _ = NewRedisClient(
//
//	fmt.Sprintf("%s:%s", RedisHost, RedisPort),
//	RedisPass)
)

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {

		return true
	},
}

type NATSProxy struct {
	URL string

	// The connection going to NATS
	EgressConnection *nats.Conn

	// Map to store multiple subscriptions
	EgressSubscriptions sync.Map

	// The set of subscribers/users that wish to communicate with the same egress route
	// Outer map: session ID to inner map
	// Inner map: WebSocket connection pointer to empty struct (used as a set)
	IngressConnections sync.Map

	// Mutex for protecting access to EgressSubscriptions
	subscriptionsMu sync.Mutex
}

func (nr *NATSProxy) Connect() error {
	// Get NATS URL from environment variable
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL // Use default if not set
	}

	// Connect to NATS
	nc, err := nats.Connect(natsURL, nats.Timeout(20*time.Second), nats.MaxPingsOutstanding(10))
	if err != nil {
		log.Println("failed to connect to NATS:", err)
		if err != nil {
			log.Printf("unable to establish connection %v to nats url %v\n", err, natsURL)
			return err
		}
	}
	nr.EgressConnection = nc
	return nil
}

// Subscribe using a unique session allows for multiple clients (users, systems - via websockets)
//
//	to send and receive data on the same subject
func (nr *NATSProxy) Subscribe(session string, subject string) (*nats.Subscription, error) {
	callback := func(m *nats.Msg) {
		value, loaded := nr.IngressConnections.Load(session)
		if !loaded {
			log.Printf("no connections are available for requested websocket session: %v\n", session)
			return
		}

		var toClose []interface{}
		sessions := value.(*sync.Map)
		sessions.Range(func(wsKey, _ interface{}) bool {
			ws := wsKey.(*websocket.Conn)
			if err := ws.WriteMessage(websocket.TextMessage, m.Data); err != nil {
				log.Printf("failed to write WebSocket message for session %s: %v", session, err)
				toClose = append(toClose, ws)
			}
			return true
		})

		// delete the connections that failed from the session
		for _, wsKey := range toClose {
			ws := wsKey.(*websocket.Conn)
			err := ws.Close()
			if err != nil {
				log.Printf("failed to close websocket connection for session %s: %v", session, err)
			}

			// delete the websocket from the list of connections within the session
			sessions.Delete(session)
		}
	}

	// Use LoadOrStore to ensure atomic check-and-set operation
	sub, loaded := nr.EgressSubscriptions.LoadOrStore(subject, &sync.Once{})
	if loaded {
		// If it's already a *nats.Subscription, return it
		if natsSubscription, ok := sub.(*nats.Subscription); ok {
			return natsSubscription, nil
		}
	}

	// If it's a *sync.Once, use it to ensure the subscription is created only once
	once, ok := sub.(*sync.Once)
	if !ok {
		return nil, fmt.Errorf("unexpected value type in EgressSubscriptions for subject: %s", subject)
	}

	var natsSubscription *nats.Subscription
	var subscribeErr error

	once.Do(func() {
		natsSubscription, subscribeErr = nr.EgressConnection.Subscribe(subject, callback)
		if subscribeErr == nil {
			// If subscription was successful, store the actual *nats.Subscription
			log.Printf("successfully subscribed to NATS subject: %v, session: %v\n", subject, session)
			nr.EgressSubscriptions.Store(subject, natsSubscription)
		} else {
			// If subscription failed, remove the sync.Once
			log.Printf("failed to subscribe to NATS subject %v, session: %v, error: %v\n", subject, session, subscribeErr)
			nr.EgressSubscriptions.Delete(subject)
		}
	})

	if subscribeErr != nil {
		return nil, subscribeErr
	}

	return natsSubscription, nil
}

func (nr *NATSProxy) JoinSession(session string, conn *websocket.Conn) bool {
	// check the ingress connections for a session store (which holds all the inbound websocket connections)
	sessionStore, _ := nr.IngressConnections.LoadOrStore(session, &sync.Map{})

	// subscribe the new inbound websocket to a session store (e.g. {websockets} => processor.state.{session}
	sessionIngressConnections := sessionStore.(*sync.Map)
	sessionIngressConnections.Store(conn, struct{}{})
	return true
}

func (nr *NATSProxy) Unsubscribe(subject string) error {
	// fetch the subscription and delete it from the list of egress connections
	log.Printf("Unsubscribing from NATS subject: %v\n", subject)
	value, loaded := nr.EgressSubscriptions.LoadAndDelete(subject)
	if !loaded {
		log.Printf("error unsubscribing from NATS, possible race condition - should not happen %v\n", subject)
		return nil
	}

	// get the subscription and unsubscribe from the nats subject
	subscription := value.(*nats.Subscription)

	// unsubscribe from the nats route
	err := subscription.Unsubscribe()

	if err != nil {
		return err
	}

	return nil
}

func (nr *NATSProxy) HandleWebsocket(session string, subject string, conn *websocket.Conn) error {
	// ensure that the subscription is established before joining the connection to the session
	_, err := nr.Subscribe(session, subject)
	if err != nil {
		log.Printf("closing websocket connection to NATS subject: %v, session: %v, conn: %v, error: %v\n",
			subject, session, conn.RemoteAddr(), err)

		// ensure to close the client connection, the nats subscription failed.
		return conn.Close()
	}

	// connection joins session, now we can send and receive data amongst participants of the session
	ok := nr.JoinSession(session, conn)
	if !ok {
		log.Printf("failed to join connection: %v, session %v\n", conn, session)

		// ensure to close the client connection, the nats subscription failed.
		err := conn.Close()
		if err != nil {
			// TODO clean up NATS subscriptions, in addition to implementing a session connections/subscription reconciliation loop
		}
		return err
	}

	return nil
}

// HandleStateSessionWebsocket TODO ensure proper closing of sockets and NATS routes
func (nr *NATSProxy) HandleStateSessionWebsocket(c *gin.Context) {
	// Get the path variable
	stateID := c.Param("state")
	if stateID == "" {
		log.Println("state id needs to be part of the path, id in /ws/:id is missing")
		return
	}

	// optional
	sessionID := c.Param("session")

	// Upgrade initial GET request to a WebSocket
	conn, err := websocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Failed to upgrade to WebSocket:", err)
		return
	}

	// use the id in your WebSocket logic
	log.Printf("WebSocket connection established for id: %s, sessionId: %s, address: %s", stateID, sessionID, conn.RemoteAddr())

	// state Session Key
	//ssk := sessionID
	session := fmt.Sprintf("%s:%s", stateID, sessionID)

	// subscribe to NATS subject for this state id
	subject := fmt.Sprintf("processor.state.%s.%s", stateID, sessionID)

	// handle websocket connection addition to session and nats subject subscription
	err = nr.HandleWebsocket(session, subject, conn)
	if err != nil {
		log.Printf("failed to handle websocket connection to NATS subject: %v, session: %v, conn: %v, error: %v\n",
			subject, session, conn, err)
	}

	// Close the NATS subscription and connection
	//log.Println("Closing NATS connection")
}

func main() {

	proxy := NATSProxy{
		URL: os.Getenv("NATS_URL"),
	}

	err := proxy.Connect()
	if err != nil {
		log.Fatalf("unable to connect to nats server: %v", err)
	}
	defer proxy.EgressConnection.Close()

	// setup gin endpoints (inbound websocket connection handlers on /ws/? path)
	router := gin.Default()
	//router.GET("/ws/:state", proxy.HandleStateSessionWebsocket)
	router.GET("/ws/:state/:session", proxy.HandleStateSessionWebsocket)

	log.Println("Starting server on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
