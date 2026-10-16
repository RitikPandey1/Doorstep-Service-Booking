// Package lock provides a distributed mutex used to serialise bookings per technician-day.
package lock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	// ErrTimeout means the lock was still held by someone else when the wait elapsed.
	ErrTimeout = errors.New("lock wait timed out")
	// ErrUnavailable means the lock backend could not be reached.
	ErrUnavailable = errors.New("lock backend unavailable")
)

// Locker acquires named locks. Release must be called once the critical section ends.
type Locker interface {
	Acquire(ctx context.Context, key string) (release func(), err error)
}

const retryInterval = 25 * time.Millisecond

// Deletes the key only if it still holds our token, so a holder whose TTL expired
// never deletes a lock that someone else has since acquired.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0`)

type RedisLocker struct {
	client *redis.Client
	ttl    time.Duration // lock auto-expires after this, in case the holder dies
	wait   time.Duration // how long Acquire keeps retrying before ErrTimeout
}

func NewRedisLocker(client *redis.Client, ttl, wait time.Duration) *RedisLocker {
	return &RedisLocker{client: client, ttl: ttl, wait: wait}
}

// Acquire polls SET key token NX PX ttl until it wins, the wait elapses (ErrTimeout),
// ctx ends, or Redis fails (ErrUnavailable).
func (l *RedisLocker) Acquire(ctx context.Context, key string) (func(), error) {
	token := uuid.NewString()
	deadline := time.Now().Add(l.wait)
	for {
		ok, err := l.client.SetNX(ctx, key, token, l.ttl).Result()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if ok {
			return func() { l.release(key, token) }, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrTimeout
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}

func (l *RedisLocker) release(key, token string) {
	// Detached context: release must run even if the request context is already cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := releaseScript.Run(ctx, l.client, []string{key}, token).Err(); err != nil {
		// The TTL frees the lock eventually; nothing else to do.
		slog.Warn("lock release failed", "key", key, "error", err)
	}
}
