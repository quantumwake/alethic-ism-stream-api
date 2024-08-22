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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type NATSProxy struct {
	NATSUrl            string
	NATSMessageHandler func(*NATSProxy, *pool.Pool, *nats.Msg)
	DataSources        sync.Map
}

func NewNATSProxy(natsUrl string, handler func(*NATSProxy, *pool.Pool, *nats.Msg)) *NATSProxy {
	return &NATSProxy{
		NATSUrl:            natsUrl,
		NATSMessageHandler: handler,
	}
}

// JoinPool a entity.go connection joins the subject pool
func (p *NATSProxy) JoinPool(subject string, websocket *websocket.Conn) (*pool.Pool, *pool.PoolConnection, error) {
	dsPool, err := p.CreateOrLoadSessionPool(subject)
	if err != nil {
		return nil, nil, fmt.Errorf("register or load pool pool failed: %v", err)
	}

	poolConnection := pool.NewPoolConnection(websocket)
	if err = dsPool.AddConnection(poolConnection); err != nil {
		return nil, nil, fmt.Errorf("register or load pool connection failed: %v", err)
	}

	return dsPool, poolConnection, err
}

func (p *NATSProxy) NewPool(subject string) (*pool.Pool, error) {
	dsPool, err := pool.NewSessionPool(p.NATSUrl, subject)
	if err != nil {
		return nil, fmt.Errorf("create new pool pool failed: %v", err)
	}
	return dsPool, nil
}

// CreateOrLoadSessionPool loads an existing pool if it exists, otherwise it creates a new one such that we can add new entity.go connections and establish downstream NATS subscription
func (p *NATSProxy) CreateOrLoadSessionPool(subject string) (*pool.Pool, error) {
	var dsPool *pool.Pool
	var err error

	value, loaded := p.DataSources.LoadOrStore(subject, &sync.Once{})
	if loaded {
		dsPool, ok := value.(*pool.Pool)
		if !ok {
			return nil, fmt.Errorf("invalid type stored for subject: %s, expected *pool.Pool", subject)
		}
		return dsPool, nil
	}

	once := value.(*sync.Once)
	once.Do(func() {
		dsPool, err = p.NewPool(subject)
		if err != nil {
			return
		}

		handler := func(msg *nats.Msg) {
			p.NATSMessageHandler(p, dsPool, msg)
		}

		sub, subErr := dsPool.Subscribe(subject, handler)
		if subErr != nil {
			err = subErr
			return
		}
		dsPool.Subscription = sub
		p.DataSources.Store(subject, dsPool)
	})

	if err != nil {
		p.DataSources.Delete(subject) // Clean up on failure
		return nil, fmt.Errorf("failed to create session pool for subject %s: %v", subject, err)
	}

	// Re-load the pool to ensure we're returning the correct instance
	value, _ = p.DataSources.Load(subject)
	dsPool, ok := value.(*pool.Pool)
	if !ok {
		return nil, fmt.Errorf("failed to load created pool for subject: %s", subject)
	}

	return dsPool, nil
}

func (p *NATSProxy) GetSubjectPath(*gin.Context) (string, error) {
	return "", nil
}

func (p *NATSProxy) UpgradeWebsocketAndJoinPool(subject string, c *gin.Context) (*pool.Pool, *pool.PoolConnection, error) {
	// Upgrade initial GET request to a WebSocket
	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
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

// HandleStateSessionWebsocket
// TODO ensure proper closing of sockets and NATS routes
// TODO ensure to verify that the user, project and datsource exist
func (p *NATSProxy) HandleStreamWebsocket(c *gin.Context) {
	// TODO verify client identity
	// Get the path variable
	state := c.Param("state")
	if state == "" {
		// TODO response 404?
		log.Println("state id needs to be part of the path, id in /ws/:state/:session is missing")
		return
	}

	// optional
	session := c.Param("session")
	if session == "" {
		// TODO response 500?
		log.Println("session needs to be part of the path, id in /ws/:id is missing")
		return
	}

	subject := fmt.Sprintf("processor.state.%s.%s", state, session)

	_, _, err := p.UpgradeWebsocketAndJoinPool(subject, c)
	if err != nil {
		// TODO response 500?
		log.Printf("failed to upgrade to entity.go: %v, subject: %v, error: %v\n\n", c.RemoteIP(), subject, err)
	}

}

func (p *NATSProxy) HandleDataSourceWebsocket(c *gin.Context) {
	ds := c.Param("ds")
	if ds == "" {
		log.Println("datasource (ds) is required. /ws/:ds is missing or invalid")
		// TODO response 404?
		return
	}

	ds = fmt.Sprintf("datasource.%s", ds)

	dsPool, wsPoolConn, err := p.UpgradeWebsocketAndJoinPool(ds, c)
	if err != nil {
		log.Printf("failed to upgrade to entity.go: %v, ds subject: %v, error: %v\n\n", c.RemoteIP(), ds, err)
	}

	// spawn a go routine that waits for replies on the websocket and responds to the originating nats request
	go wsPoolConn.WaitResourceReply(dsPool, wsPoolConn)
}
