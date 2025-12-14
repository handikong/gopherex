package etcd

import (
	"context"
	"encoding/json"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
	"gopherex.com/pkg/register"
)

type EtcdRegister struct {
	client        *clientv3.Client
	basePath      string // 比如 "/gopherex/services"
	ttl           int64  // 租约秒数
	keepaliveChan <-chan *clientv3.LeaseKeepAliveResponse
	leaseID       clientv3.LeaseID
}

func NewEtcdRegister(c *clientv3.Client, bashPath string, ttl int64) *EtcdRegister {
	return &EtcdRegister{
		client:   c,
		basePath: bashPath,
		ttl:      ttl,
	}
}

func (e *EtcdRegister) getKey(ins *register.Instance) string {
	return fmt.Sprintf("%s/%s/%s", e.basePath, ins.Name, ins.ID)
}

func (e *EtcdRegister) Register(ctx context.Context, ins *register.Instance) error {
	// 进行租约
	grandRes, err := e.client.Grant(ctx, e.ttl)
	if err != nil {
		return err
	}
	// 获取租约的id
	e.leaseID = grandRes.ID
	// 将内容写入kv
	val, err := json.Marshal(ins)
	if err != nil {
		return err
	}
	key := e.getKey(ins)
	_, err = e.client.Put(ctx, key, string(val), clientv3.WithLease(e.leaseID))
	if err != nil {
		return err
	}
	// 生成一个心跳
	keepliveChat, err := e.client.KeepAlive(ctx, e.leaseID)
	if err != nil {
		return err
	}
	e.keepaliveChan = keepliveChat
	go e.keepliveChan(ctx)
	return nil
}

func (e *EtcdRegister) UnRegister(ctx context.Context, ins *register.Instance) error {
	// 提出key  接触续约
	_, err := e.client.Delete(ctx, e.getKey(ins))
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	_, err = e.client.Revoke(ctx, e.leaseID)
	if err != nil {
		return fmt.Errorf("Revoke instance: %w", err)
	}
	return nil
}

func (e *EtcdRegister) keepliveChan(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// 为什么不在这里注销呢？
			return
		case <-e.keepaliveChan:
			//自动续约 无需手动
			//logger.Info(ctx, "etcd lease keepalive success", zap.Int64("leaseID", int64(e.leaseID)))
		}
	}
}
