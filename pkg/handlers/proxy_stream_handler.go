package handlers

import (
	"alethic-ism-stream-api/pkg/pool"
	"alethic-ism-stream-api/pkg/proxy"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"log"
)

type StreamProxyAdapter struct {
	proxy.NATSProxyAdapter
}

// OnConnect

// OnMessage processes nats messages by forwarding the raw msg to all connections within the pool.
// When messages consumed from the NATS subject, as defined by the (as handled by the pool.Pool),
// TODO ensure proper closing of sockets and NATS routes
// TODO ensure to verify that the user, project and datsource exist
func (s *StreamProxyAdapter) OnMessage(proxy *proxy.NATSProxy, dsPool *pool.Pool, msg *nats.Msg) {
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

// func (p *StreamProxyAdapter) OnMessage(proxy *NATSProxy, ctx, )

// HandleStateSessionWebsocket
// TODO ensure proper closing of sockets and NATS routes
// TODO ensure to verify that the user, project and datsource exist
func (s *StreamProxyAdapter) OnConnect(proxy *proxy.NATSProxy, ctx *gin.Context) {
	// TODO verify client identity
	// Get the path variable
	state := ctx.Param("state")
	if state == "" {
		// TODO response 404?
		log.Println("state id needs to be part of the path, id in /ws/:state/:session is missing")
		return
	}

	// optional
	var subject string = ""
	session := ctx.Param("session")
	if session == "" {
		subject = fmt.Sprintf("processor.state.%s", state)
	} else {
		subject = fmt.Sprintf("processor.state.%s.%s", state, session)
	}

	_, _, err := proxy.UpgradeWebsocketAndJoinPool(subject, ctx)
	if err != nil {
		// TODO response 500?
		log.Printf("failed to upgrade to entity.go: %v, subject: %v, error: %v\n\n", ctx.RemoteIP(), subject, err)
	}
}
