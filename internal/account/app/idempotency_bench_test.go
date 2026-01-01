package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// 假设你已有：db *sql.DB, DoOnceTx(...), 以及表 idempotency 已存在
// 以及你已有 resetTables(t, db) / 或者自己用 TRUNCATE。
var db, _ = sql.Open("mysql", "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?parseTime=true")

func BenchmarkIdem_NoContention_NewKey(b *testing.B) {
	_, _ = db.Exec(`TRUNCATE TABLE idempotency;`)

	scope := "user:1001"
	hash := sha256.Sum256([]byte("amount=777"))

	var executed int32

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			reqID := fmt.Sprintf("req-%d", i) // 每次不同 key，几乎无竞争

			_, err := DoOnceTx(context.Background(), db, scope, reqID, hash,
				func(ctx context.Context, tx *sql.Tx) (string, error) {
					atomic.AddInt32(&executed, 1)
					return "OK", nil
				},
			)
			if err != nil {
				b.Logf("err: %v", err)
			}
		}
	})

	_ = executed // 防止编译器优化
}

func BenchmarkIdem_HotKey_Contended_NoRetry(b *testing.B) {
	// 这个 benchmark 在高并发下可能会遇到 1213 并直接失败（故意保留，观察死锁概率）
	_, _ = db.Exec(`TRUNCATE TABLE idempotency;`)

	scope := "user:1001"
	reqID := "req-hot"
	hash := sha256.Sum256([]byte("amount=777"))

	var executed int32

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := DoOnceTx(context.Background(), db, scope, reqID, hash,
				func(ctx context.Context, tx *sql.Tx) (string, error) {
					atomic.AddInt32(&executed, 1)
					// 放大竞争窗口（可按需调小）
					time.Sleep(100 * time.Microsecond)
					return "OK", nil
				},
			)
			if err != nil {
				b.Logf("err: %v", err)
			}
		}
	})
}

func BenchmarkIdem_HotKey_Contended_WithRetry(b *testing.B) {
	// 推荐跑这个：把 1213/1205/40001 作为可重试错误吸收掉
	_, _ = db.Exec(`TRUNCATE TABLE idempotency;`)

	scope := "user:1001"
	reqID := "req-hot"
	hash := sha256.Sum256([]byte("amount=777"))

	var executed int32

	isRetryable := func(err error) bool {
		var me *mysql.MySQLError
		if errors.As(err, &me) {
			if me.Number == 1213 || me.Number == 1205 {
				return true
			}
			if me.Message == "40001" {
				return true
			}
		}
		return false
	}

	call := func(ctx context.Context, fn func(context.Context, *sql.Tx) (string, error)) (string, error) {
		var last error
		for attempt := 0; attempt < 8; attempt++ {
			out, err := DoOnceTx(ctx, db, scope, reqID, hash, fn)
			if err == nil {
				return out, nil
			}
			if !isRetryable(err) {
				return "", err
			}
			last = err
			// 极小 backoff（bench 不要太大，不然测不到 DB 本身）
			time.Sleep(time.Duration(1<<attempt) * time.Microsecond)
		}
		return "", last
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := call(context.Background(), func(ctx context.Context, tx *sql.Tx) (string, error) {
				atomic.AddInt32(&executed, 1)
				time.Sleep(100 * time.Microsecond)
				return "OK", nil
			})
			if err != nil {
				b.Logf("err: %v", err)
			}
		}
	})

	_ = executed
}
