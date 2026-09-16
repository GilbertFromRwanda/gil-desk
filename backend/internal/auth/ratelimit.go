package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter is a fixed-window counter backed by Redis (planner task
// G-18) — INCR+EXPIRE, the standard pattern for this, not something that
// needs a library. Currently applied to login attempts, keyed by email,
// to slow down password guessing against one account; it isn't yet
// applied to registration (a different threat model — account-creation
// spam, not credential guessing) or keyed by IP (no reverse-proxy
// topology exists yet to trust a client-supplied IP header from).
type RateLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

func NewRateLimiter(client *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{client: client, limit: limit, window: window}
}

// Allow increments the attempt counter for key and reports whether the
// caller is still within the limit for the current window.
func (r *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	fullKey := "ratelimit:" + key
	count, err := r.client.Incr(ctx, fullKey).Result()
	if err != nil {
		return false, fmt.Errorf("increment rate limit counter for %s: %w", key, err)
	}
	if count == 1 {
		if err := r.client.Expire(ctx, fullKey, r.window).Err(); err != nil {
			return false, fmt.Errorf("set rate limit expiry for %s: %w", key, err)
		}
	}
	return count <= int64(r.limit), nil
}

// Reset clears the counter for key — call on a successful login so a
// legitimate user's earlier failed attempts don't count against them
// once they get it right.
func (r *RateLimiter) Reset(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, "ratelimit:"+key).Err(); err != nil {
		return fmt.Errorf("reset rate limit counter for %s: %w", key, err)
	}
	return nil
}
