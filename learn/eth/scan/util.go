package scan

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/redis/go-redis/v9"
)

func cursorInt64ToUint64(cursor int64) (uint64, error) {
	if cursor < 0 {
		return 0, fmt.Errorf("cursor must be >= 0, got %d", cursor)
	}
	return uint64(cursor), nil
}

func cursorUint64ToInt64(cursor uint64) (int64, error) {
	if cursor > math.MaxInt64 {
		return 0, fmt.Errorf("cursor overflows int64: %d", cursor)
	}
	return int64(cursor), nil
}

func isRedisXAddIDTooSmallErr(err error) bool {
	if err == nil {
		return false
	}
	// Redis returns this error when the provided ID is <= last stream ID.
	// It can be caused by retries (same ID) or out-of-order inserts.
	return strings.Contains(err.Error(), "equal or smaller than the target stream top item")
}

func xaddIDExists(ctx context.Context, rs *redis.Client, stream, id string) (bool, error) {
	entries, err := rs.XRange(ctx, stream, id, id).Result()
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}
