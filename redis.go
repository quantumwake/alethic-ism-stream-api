package main

import (
	"fmt"
	"github.com/gomodule/redigo/redis"
	"time"
)

// RedisClient is a struct that manages the Redis connection
type RedisClient struct {
	//conn redis.Conn
	redisPool *redis.Pool
}

func newPool(server string, password string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp",
				server,
				redis.DialPassword(password))
		},

		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

// NewRedisClient initializes a new Redis client
func NewRedisClient(address string, password string) (*RedisClient, error) {
	pool := newPool(address, password)
	//c := redissearch.NewClientFromPool(p, index)

	//conn, err := redis.Dial("tcp", address, redis.DialOption{"password": ""})
	//if err != nil {
	//	return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	//}

	//return &RedisClient{conn: conn}, nil
	return &RedisClient{redisPool: pool}, nil
}

// Close closes the Redis connection
func (client *RedisClient) Close() error {
	//client.conn.Close()
	err := client.redisPool.Close()
	if err != nil {
		return err
	}
	return nil
}

// StoreList stores a list of strings in Redis under the specified key
func (client *RedisClient) StoreList(key string, strings []string) error {
	for _, str := range strings {
		conn := client.redisPool.Get()
		_, err := conn.Do("RPUSH", key, str)
		if err != nil {
			return fmt.Errorf("failed to RPUSH to Redis: %w", err)
		}
	}
	return nil
}

// RetrieveList retrieves a list of strings from Redis by key
func (client *RedisClient) RetrieveList(key string) ([]string, error) {
	conn := client.redisPool.Get()
	list, err := redis.Strings(conn.Do("LRANGE", key, 0, -1))
	if err != nil {
		return nil, fmt.Errorf("failed to LRANGE from Redis: %w", err)
	}
	return list, nil
}
