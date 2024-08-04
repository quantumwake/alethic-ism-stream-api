package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"log"
	"net/http"
	"os"
	"time"
)

// Define a WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func websocketHandler(c *gin.Context) {
	// Get the path variable
	id := c.Param("id")
	if id == "" {
		log.Println("state id needs to be part of the path, id in /ws/:id is missing")
		return
	}

	// Use the id in your WebSocket logic
	log.Printf("WebSocket connection established for id: %s", id)

	// Upgrade initial GET request to a WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Failed to upgrade to WebSocket:", err)
		return
	}

	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {

		}
	}(conn)

	// Get NATS URL from environment variable
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL // Use default if not set
	}

	// Store the connection in the map
	//stateToConn[stateId] = conn

	// Subscribe to NATS subject for this stateId
	subject := fmt.Sprintf("processor.state.%s", id)

	// Connect to NATS
	nc, err := nats.Connect(natsURL, nats.Timeout(20*time.Second), nats.MaxPingsOutstanding(10))
	if err != nil {
		log.Println("Failed to connect to NATS:", err)
		err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "NATS connection failed"))
		if err != nil {
			return
		}
		return
	}
	defer nc.Close()

	// Create a channel to handle NATS messages
	//msgCh := make(chan *nats.Msg, 64)

	// Subscribe to a NATS subject
	//sub, err := nc.ChanSubscribe(subject, msgCh)
	//if err != nil {
	//	log.Println("Failed to subscribe to subject:", err)
	//	err := conn.WriteMessage(
	//		websocket.CloseMessage,
	//		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "NATS subscription failed"))
	//	if err != nil {
	//		return
	//	}
	//	return
	//}

	_, err = nc.Subscribe(subject, func(m *nats.Msg) {
		message := m.Data
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Println("Failed to write WebSocket message:", err)
			return
		}
	})

	if err != nil {
		return
	}
	//
	//defer func(sub *nats.Subscription) {
	//	err := sub.Unsubscribe()
	//	if err != nil {
	//
	//	}
	//}(sub)
	//
	//// Handle incoming WebSocket and NATS messages
	//done := make(chan struct{})
	//go func() {
	//	defer close(done)
	//	for {
	//		select {
	//		case msg := <-msgCh:
	//			message := msg.Data
	//			//print(message)
	//			//
	//			//message = msg.Data
	//
	//			print(string(message))
	//			//print(fmt.Println("*** %s", string(message)))
	//			//if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
	//			//	log.Println("Failed to write WebSocket message:", err)
	//			//	return
	//			//}
	//		}
	//	}
	//}()

	// Wait for client disconnect or error
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			log.Println("WebSocket disconnected:", err)
			break
		}
	}

	// Close the NATS subscription and connection
	log.Println("Closing NATS connection")
	nc.Close()
}

func main() {
	router := gin.Default()
	router.GET("/ws/:id", websocketHandler)

	log.Println("Starting server on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
