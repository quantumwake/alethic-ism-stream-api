package proxy

import (
	"alethic-ism-stream-api/pkg/pool"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"log"
	"net/http"
	"sync"
)

// WebsocketUpgrade upgrader for inbound gin http requests.
var WebsocketUpgrade = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NATSProxyAdapter defines the function signature for when a new websocket is established.
type NATSProxyAdapter interface {
	OnMessage(proxy *NATSProxy, dsPool *pool.Pool, msg *nats.Msg)
	OnConnect(proxy *NATSProxy, ctx *gin.Context)
}

// NATSProxy encapsulates the general management of web connections and NATS subjects, as defined by pool.Pool.
type NATSProxy struct {
	// NATSUrl defines the primary nats connection url
	NATSUrl string

	Handler NATSProxyAdapter
	// NATSMessageHandler defines the handling function when messages are consumed on the NATS subject.
	//NATSMessageHandler func(*NATSProxy, *pool.Pool, *nats.Msg)

	// Pools defines a list of subject to pool associations, where each pool maintains a list of websocket connections associated to a NATS subject.
	Pools sync.Map
}

// NewNATSProxy creates a new NATSProxy using a pool creation handling method.
func NewNATSProxy(natsUrl string, handler NATSProxyAdapter) *NATSProxy {
	return &NATSProxy{
		NATSUrl: natsUrl,
		Handler: handler,
	}
}

// JoinPool establishes a relation between the http upgraded websocket with a NATS subject.
// this ensures that any data consumed on the NATS subject is propagated to the related set of active websocket connections.
func (p *NATSProxy) JoinPool(subject string, websocket *websocket.Conn) (*pool.Pool, *pool.PoolConnection, error) {

	// create a new pool for given subject, if pool does not exist, otherwise return existing pool.
	dsPool, err := p.LoadOrCreatePool(subject)
	if err != nil {
		return nil, nil, fmt.Errorf("register or load pool pool failed: %v", err)
	}
	// add the websocket connection to the set of active connections for this particular nats subject.
	poolConnection := pool.NewPoolConnection(websocket)
	if err = dsPool.AddConnection(poolConnection); err != nil {
		return nil, nil, fmt.Errorf("register or load pool connection failed: %v", err)
	}

	return dsPool, poolConnection, err
}

// LoadOrCreatePool return existing pool if exists, otherwise creates a new websocket connection pool.
func (p *NATSProxy) LoadOrCreatePool(subject string) (*pool.Pool, error) {
	var dsPool *pool.Pool
	var err error

	//  load or create a new pool, atomically.
	value, loaded := p.Pools.LoadOrStore(subject, &sync.Once{})
	if loaded {
		dsPool, ok := value.(*pool.Pool)
		if !ok {
			return nil, fmt.Errorf("invalid type stored for subject: %s, expected *pool.Pool", subject)
		}
		return dsPool, nil
	}

	once := value.(*sync.Once)
	once.Do(func() {
		dsPool, err = pool.NewPool(p.NATSUrl, subject)
		if err != nil {
			return
		}

		handler := func(msg *nats.Msg) {
			p.Handler.OnMessage(p, dsPool, msg)
		}

		sub, subErr := dsPool.Subscribe(subject, handler)
		if subErr != nil {
			err = subErr
			return
		}
		dsPool.Subscription = sub
		p.Pools.Store(subject, dsPool)
	})

	if err != nil {
		p.Pools.Delete(subject) // Clean up on failure
		return nil, fmt.Errorf("failed to create session pool for subject %s: %v", subject, err)
	}

	// Re-load the pool to ensure we're returning the correct instance
	value, _ = p.Pools.Load(subject)
	dsPool, ok := value.(*pool.Pool)
	if !ok {
		return nil, fmt.Errorf("failed to load created pool for subject: %s", subject)
	}

	return dsPool, nil
}

// UpgradeWebsocketAndJoinPool upgrades the current gin http request to a websocket, and joins the subject pool.
func (p *NATSProxy) UpgradeWebsocketAndJoinPool(subject string, c *gin.Context) (*pool.Pool, *pool.PoolConnection, error) {
	// Upgrade initial GET request to a WebSocket
	wsConn, err := WebsocketUpgrade.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to upgrade to entity.go: %v, error: %v:\n", wsConn.RemoteAddr(), err)
	}

	// use the id in your WebSocket logic
	log.Printf("entity.go connection established for subject: %v, address: %v", subject, wsConn.RemoteAddr())

	// join the inbound entity.go connection to the pool, which has inbound/output functionality to the ISM system via NATS
	dsPool, wsPoolConn, err := p.JoinPool(subject, wsConn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add entity.go connection for subject: %v, conn: %v, error: %v\n", subject, wsConn.RemoteAddr(), err)
	}

	return dsPool, wsPoolConn, err
}
