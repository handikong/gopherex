package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/register"
)

const Scheme = "gopherex"

type etcdBuilder struct {
	cli      *clientv3.Client
	basePath string
}

func NewBuilder(cli *clientv3.Client, basePath string) resolver.Builder {
	return &etcdBuilder{
		cli:      cli,
		basePath: basePath,
	}
}

func (e *etcdBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {

	// target.Endpoint() 就是服务名，例如 "wallet-service"
	serviceName := target.Endpoint()
	ctx, cancel := context.WithCancel(context.Background())

	r := &etcdResolver{
		cli:         e.cli,
		basePath:    e.basePath,
		serviceName: serviceName,
		cc:          cc,
		ctx:         ctx,
		cancel:      cancel,
	}
	go r.watch()

	// 初始化拉一次
	if err := r.updateAddresses(); err != nil {
		// 这里可以打日志，但通常不直接 fail
		fmt.Printf("update addresses failed: %v\n", err)
	}

	return r, nil

}
func (e *etcdBuilder) Scheme() string {
	return Scheme
}

type etcdResolver struct {
	cli         *clientv3.Client
	basePath    string
	serviceName string

	cc resolver.ClientConn

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	addresses map[string]register.Instance // key: ins.ID
}

func (r *etcdResolver) ResolveNow(opts resolver.ResolveNowOptions) {
	// gRPC 会偶尔调用这个函数要求立即解析，我们简单粗暴地重新拉一次
	_ = r.updateAddresses()
}

func (r *etcdResolver) Close() {
	r.cancel()
}

func (r *etcdResolver) prefix() string {
	return fmt.Sprintf("%s/%s/", r.basePath, r.serviceName)
}

func (r *etcdResolver) updateAddresses() error {
	resp, err := r.cli.Get(r.ctx, r.prefix(), clientv3.WithPrefix())
	if err != nil {
		return err
	}

	newAddrs := make(map[string]register.Instance)
	for _, kv := range resp.Kvs {
		var ins register.Instance
		if err := json.Unmarshal(kv.Value, &ins); err != nil {
			continue
		}
		newAddrs[ins.ID] = ins
	}

	r.mu.Lock()
	r.addresses = newAddrs
	r.mu.Unlock()

	// 转换为 gRPC recognisable addresses
	addrs := make([]resolver.Address, 0, len(newAddrs))
	for _, ins := range newAddrs {
		addrs = append(addrs, resolver.Address{
			Addr: ins.Addr,
		})
	}
	r.cc.UpdateState(resolver.State{
		Addresses: addrs,
	})

	return nil
}

func (r *etcdResolver) watch() {
	watchCh := r.cli.Watch(r.ctx, r.prefix(), clientv3.WithPrefix())
	for {
		select {
		case <-r.ctx.Done():
			return
		case wresp, ok := <-watchCh:
			logger.Info(r.ctx, "wathch到数据了")
			if !ok {
				return
			}
			if wresp.Err() != nil {
				// 可以打日志
				continue
			}
			// 有变更时，简单粗暴刷新一遍
			_ = r.updateAddresses()
		}
	}
}
