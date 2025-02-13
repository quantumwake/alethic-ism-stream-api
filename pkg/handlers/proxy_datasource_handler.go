package handlers

import (
	"alethic-ism-stream-api/pkg/model"
	"alethic-ism-stream-api/pkg/pool"
	"alethic-ism-stream-api/pkg/proxy"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"log"
)

type DSProxyAdapter struct {
	proxy.NATSProxyAdapter
}

func (h *DSProxyAdapter) OnConnect(proxy *proxy.NATSProxy, ctx *gin.Context) {
	ds := ctx.Param("ds")
	if ds == "" {
		log.Println("datasource (ds) is required. /ws/:ds is missing or invalid")
		// TODO response 404?
		return
	}

	ds = fmt.Sprintf("datasource.%s", ds)

	dsPool, wsPoolConn, err := proxy.UpgradeWebsocketAndJoinPool(ds, ctx)
	if err != nil {
		log.Printf("failed to upgrade to entity.go: %v, ds subject: %v, error: %v\n\n", ctx.RemoteIP(), ds, err)
	}

	// spawn a go routine that waits for replies on the websocket and responds to the originating nats request
	go wsPoolConn.WaitResourceReply(dsPool, wsPoolConn)
}

// OnMessage defines the handling implementation with respect to a registered datasource.
// This handler uses a Request-Reply mode, as associated between a Websocket and a NATS subject.
func (h *DSProxyAdapter) OnMessage(_ *proxy.NATSProxy, dsPool *pool.Pool, msg *nats.Msg) {
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
