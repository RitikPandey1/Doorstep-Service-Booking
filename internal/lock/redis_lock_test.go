package lock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// newRedis starts a throwaway Redis container (requires Docker; skipped with -short).
func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker")
	}
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	url, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisLocker(t *testing.T) {
	client := newRedis(t)
	ctx := context.Background()

	t.Run("second acquirer times out while lock is held", func(t *testing.T) {
		l := NewRedisLocker(client, 5*time.Second, 100*time.Millisecond)
		release, err := l.Acquire(ctx, "k1")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := l.Acquire(ctx, "k1"); !errors.Is(err, ErrTimeout) {
			t.Fatalf("err = %v, want ErrTimeout", err)
		}
	})

	t.Run("waiter gets the lock after release", func(t *testing.T) {
		l := NewRedisLocker(client, 5*time.Second, time.Second)
		release, err := l.Acquire(ctx, "k2")
		if err != nil {
			t.Fatal(err)
		}
		time.AfterFunc(50*time.Millisecond, release)
		release2, err := l.Acquire(ctx, "k2")
		if err != nil {
			t.Fatalf("waiter: %v", err)
		}
		release2()
	})

	t.Run("expired holder's release does not delete the new holder's lock", func(t *testing.T) {
		short := NewRedisLocker(client, 50*time.Millisecond, 0)
		staleRelease, err := short.Acquire(ctx, "k3")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond) // TTL expires

		l := NewRedisLocker(client, 5*time.Second, 0)
		release, err := l.Acquire(ctx, "k3")
		if err != nil {
			t.Fatalf("new holder: %v", err)
		}
		defer release()

		staleRelease()
		if n := client.Exists(ctx, "k3").Val(); n != 1 {
			t.Fatal("stale release deleted the new holder's lock")
		}
	})

	t.Run("mutual exclusion under concurrency", func(t *testing.T) {
		l := NewRedisLocker(client, 5*time.Second, 5*time.Second)
		var inside, maxInside, done atomic.Int32
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				release, err := l.Acquire(ctx, "k4")
				if err != nil {
					t.Error(err)
					return
				}
				n := inside.Add(1)
				for {
					m := maxInside.Load()
					if n <= m || maxInside.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				done.Add(1)
				release()
			}()
		}
		wg.Wait()
		if maxInside.Load() != 1 || done.Load() != 20 {
			t.Fatalf("max holders = %d, completed = %d; want 1 and 20", maxInside.Load(), done.Load())
		}
	})

	t.Run("unreachable redis is ErrUnavailable", func(t *testing.T) {
		dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
		defer dead.Close()
		if _, err := NewRedisLocker(dead, time.Second, time.Second).Acquire(ctx, "k5"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})
}
