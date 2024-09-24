package main

import (
	"alethic-ism-stream-api/pkg/model"
	"alethic-ism-stream-api/pkg/pool"
	"alethic-ism-stream-api/pkg/proxy"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"log"
	"os"
)

func requestReplyHandler(p *proxy.NATSProxy, dsPool *pool.Pool, msg *nats.Msg) {
	wsConn, err := dsPool.GetRandomConnection()
	if err != nil {
		log.Printf("Error getting connection from pool: %s", err)
		// TODO 500?
		return
	}

	var request model.ProcessRequest
	err = json.Unmarshal(msg.Data, &request)
	if err != nil {
		// TODO 500
		return
	}

	// track this in the request (as an internal variable) such that we can msg.Respond(...) to the request->msg
	request.NATSMessage = msg

	// TODO validate request id }
	for {
		err := wsConn.ResourceRequest(request)

		if err != nil {
			log.Printf("failed to write message to remote address: %v, pool: %v, error: %v", wsConn, msg.Subject, err)
			dsPool.RemoveConnection(wsConn)

			wsConn, err = dsPool.GetRandomConnection()
			if err != nil {
				log.Printf("error getting connection from pool: %s", err)
				break // if reached this point, no more websockets to write data to
			}
			continue // if reached this point, we want to try the next socket selected above
		}
		break // if reached this point, the websocket send was successful
	}
}

func streamHandler(p *proxy.NATSProxy, dsPool *pool.Pool, msg *nats.Msg) {
	// write data to all sockets
	dsPool.Synchronous.RLock()
	var toClose []*pool.PoolConnection
	for wsConn, _ := range dsPool.Connections {
		if err := wsConn.WriteText(msg.Data); err != nil {
			log.Printf("failed to write message to remote address: %v, pool: %v, error: %v", wsConn, msg.Subject, err)
			toClose = append(toClose, wsConn)
		}
	}
	dsPool.Synchronous.RUnlock()

	// delete connections that failed write
	dsPool.RemoveConnections(toClose)
}

func main() {

	var natsURL, ok = os.LookupEnv("NATS_URL")
	if !ok {
		natsURL = "nats://localhost:4222"
	}

	proxyStreamProxy := proxy.NewNATSProxy(natsURL, streamHandler)
	dataSourceProxy := proxy.NewNATSProxy(natsURL, requestReplyHandler)

	// setup gin endpoints (inbound entity.go connection handlers on /ws/? path)
	router := gin.Default()
	//router.GET("/ws/:state", proxy.HandleStateSessionWebsocket)
	router.GET("/ws/stream/:state/:session", proxyStreamProxy.HandleStateSessionWebsocket)
	router.GET("/ws/stream/:state", proxyStreamProxy.HandleStateWebsocket)
	router.GET("/ws/ds/:ds", dataSourceProxy.HandleDataSourceWebsocket)

	log.Println("Starting server on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
