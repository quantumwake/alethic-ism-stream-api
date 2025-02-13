package main

import (
	"alethic-ism-stream-api/pkg/handlers"
	"alethic-ism-stream-api/pkg/proxy"
	"github.com/gin-gonic/gin"
	"log"
	"os"
)

// WithProxy wrapper function that adds the proxy parameter
func WithProxy(proxy *proxy.NATSProxy) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		proxy.Handler.OnConnect(proxy, ctx)
	}
}

func main() {

	var natsURL, ok = os.LookupEnv("NATS_URL")
	if !ok {
		natsURL = "nats://localhost:4222"
	}

	dsProxy := proxy.NewNATSProxy(natsURL, &handlers.DSProxyAdapter{})
	streamProxy := proxy.NewNATSProxy(natsURL, &handlers.StreamProxyAdapter{})

	// setup gin endpoints (inbound entity.go connection handlers on /ws/? path)
	router := gin.Default()

	router.GET("/ws/stream/:state/:session", WithProxy(streamProxy))
	router.GET("/ws/stream/:state", WithProxy(streamProxy))
	router.GET("/ws/ds/:ds", WithProxy(dsProxy))

	log.Println("Starting server on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
