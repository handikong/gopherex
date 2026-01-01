package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

var (
	ErrInProgress = errors.New("request in progress, retry later")
	ErrConflict   = errors.New("idempotency key conflict (different parameters)")
)

type IdemStatus int

const (
	Processing IdemStatus = 1
	Committed  IdemStatus = 2
	Failed     IdemStatus = 3
)

type IdemRecord struct {
	Status   IdemStatus
	Hash     [32]byte
	Response []byte
	ErrorMsg string
}

func DoOnceTx[T any](
	ctx context.Context,
	db *sql.DB,
	scope, requestID string,
	reqHash [32]byte,
	fn func(ctx context.Context, tx *sql.Tx) (T, error), // 注意：fn 只做 DB 操作
) (T, error) {
	var zero T
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1) 抢占（在事务里）
	_, err = tx.ExecContext(ctx, `
    INSERT INTO idempotency(scope, request_id, request_hash, status)
    VALUES(?,?,?,1)
  `, scope, requestID, reqHash[:])
	if err != nil {
		// 已存在：读状态并决定返回什么（同样在 tx 里读）
		// 这里只给最小：如果已 COMMITTED 就返回缓存 response
		// 你也可以扩展：PROCESSING => ErrInProgress；FAILED => 返回旧错误
		var status int
		var hashBytes []byte
		var resp sql.NullString
		var errMsg string
		e := tx.QueryRowContext(ctx, `
      SELECT status, request_hash, response_json, error_msg
      FROM idempotency WHERE scope=? AND request_id=? FOR UPDATE
    `, scope, requestID).Scan(&status, &hashBytes, &resp, &errMsg)
		if e != nil {
			return zero, e
		}
		var h [32]byte
		copy(h[:], hashBytes)
		if h != reqHash {
			return zero, ErrConflict
		}
		if status == 2 && resp.Valid {
			var out T
			if e := json.Unmarshal([]byte(resp.String), &out); e != nil {
				return zero, e
			}
			return out, nil
		}
		return zero, ErrInProgress
	}

	// 2) 执行业务（DB 内副作用）
	out, err := fn(ctx, tx)
	if err != nil {
		// 可选：你要不要记录 FAILED？
		// 最小做法：直接回滚即可，下一次重试会重新执行（适合“纯DB可回滚”的副作用）
		return zero, err
	}

	// 3) 标记成功（同一事务）
	b, _ := json.Marshal(out)
	_, err = tx.ExecContext(ctx, `
    UPDATE idempotency
    SET status=2, response_json=?, error_msg=''
    WHERE scope=? AND request_id=?
  `, b, scope, requestID)
	if err != nil {
		return zero, err
	}

	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func readIdem(ctx context.Context, db *sql.DB, scope, requestID string) (IdemRecord, error) {
	var rec IdemRecord
	var hashBytes []byte
	var resp sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT status, request_hash, response_json, error_msg
		FROM idempotency
		WHERE scope=? AND request_id=?
	`, scope, requestID).Scan(&rec.Status, &hashBytes, &resp, &rec.ErrorMsg)
	if err != nil {
		return rec, err
	}
	copy(rec.Hash[:], hashBytes)
	if resp.Valid {
		rec.Response = []byte(resp.String)
	}
	return rec, nil
}
