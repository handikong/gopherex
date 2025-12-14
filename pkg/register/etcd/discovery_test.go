package etcd_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"gopherex.com/pkg/register/etcd"
)

func TestDiscovery(t *testing.T) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"127.0.0.1:12379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("connect etcd: %v", err)
	}
	defer cli.Close()
	ctx := context.Background()
	res, err := etcd.Discovery(ctx, cli, "/gopherex/services", "wallet-service")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(etcd.PickOne(res).Addr)
}
