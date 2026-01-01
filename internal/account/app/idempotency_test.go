package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type ChargeResult struct {
	ChargeID string `json:"charge_id"`
	Amount   int64  `json:"amount"`
}

func TestDoOnce(t *testing.T) {
	db, _ := sql.Open("mysql", "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?parseTime=true")
	defer db.Close()

	ctx := context.Background()

	scope := "user:1002"
	requestID := "req-003"
	reqHash := sha256.Sum256([]byte("amount=102"))

	// 第一次：会执行 fn
	r1, err := DoOnceTx(ctx, db, scope, requestID, reqHash, func(ctx context.Context, tx *sql.Tx) (ChargeResult, error) {
		time.Sleep(200 * time.Millisecond) // 模拟副作用耗时
		return ChargeResult{ChargeID: "ch_abc", Amount: 100}, nil
	})
	fmt.Println("first:", r1, err)

	// 第二次重试：不会执行 fn，直接返回缓存结果
	r2, err := DoOnceTx(ctx, db, scope, requestID, reqHash, func(ctx context.Context, tx *sql.Tx) (ChargeResult, error) {
		return ChargeResult{}, errors.New("should not run")
	})
	fmt.Println("retry:", r2, err)

	// 同 request_id 不同参数：冲突
	badHash := sha256.Sum256([]byte("amount=999"))
	_, err = DoOnceTx(ctx, db, scope, requestID, badHash, func(ctx context.Context, tx *sql.Tx) (ChargeResult, error) {
		return ChargeResult{ChargeID: "ch_bad", Amount: 999}, nil
	})
	fmt.Println("conflict:", err)

}

type EffectResult struct {
	EffectID int64  `json:"effect_id"`
	Note     string `json:"note"`
}

func TestIdempotency_ConcurrentSameKey_ExecuteOnce(t *testing.T) {
	// 只保留这张表
	db, _ := sql.Open("mysql", "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?parseTime=true")
	defer db.Close()

	_, err := db.Exec(`TRUNCATE TABLE idempotency;`)
	if err != nil {
		t.Fatal(err)
	}

	scope := "user:1001"
	reqID := "req-concurrent"
	hash := sha256.Sum256([]byte("amount=777"))

	const N = 30
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)

	var executed int32
	results := make([]string, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start

			// fn 只返回一个结果，同时把 executed++ 用来验证“只执行一次”
			res, err := DoOnceTx(context.Background(), db, scope, reqID, hash,
				func(ctx context.Context, tx *sql.Tx) (string, error) {
					atomic.AddInt32(&executed, 1)
					time.Sleep(200 * time.Millisecond) // 放大并发窗口
					return "OK", nil
				},
			)

			results[i] = res
			errs[i] = err
		}()
	}

	close(start)
	wg.Wait()

	// 1) 所有请求都成功
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Logf("goroutine %d err: %v", i, errs[i])
		}
	}

	// 2) fn 只执行了一次
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("expected executed=1, got %d", executed)
	}

	// 3) 所有人拿到同样结果
	for i := 0; i < N; i++ {
		if results[i] != "OK" {
			t.Logf("goroutine %d result=%q", i, results[i])
		}
	}

	// 4) 表里只有一条记录，并且是 COMMITTED
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM idempotency`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 row in idempotency, got %d", cnt)
	}
	var status int
	if err := db.QueryRow(`SELECT status FROM idempotency WHERE scope=? AND request_id=?`, scope, reqID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != 2 { // 2=COMMITTED
		t.Fatalf("expected status=2(COMMITTED), got %d", status)
	}
}
