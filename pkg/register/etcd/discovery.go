package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"

	clientv3 "go.etcd.io/etcd/client/v3"
	"gopherex.com/pkg/register"
)

func Discovery(ctx context.Context, client *clientv3.Client, bashPath string, serviceName string) ([]register.Instance, error) {
	// 拼出前缀Key
	prefixKey := fmt.Sprintf("%s/%s", bashPath, serviceName)
	res, err := client.Get(ctx, prefixKey, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	var resInstance = []register.Instance{}
	for _, kv := range res.Kvs {
		// 对val进行json反编译
		var instance = register.Instance{}
		err := json.Unmarshal(kv.Value, &instance)
		if err != nil {
			continue
		}
		resInstance = append(resInstance, instance)
	}
	return resInstance, nil
}

func PickOne(instances []register.Instance) *register.Instance {
	if len(instances) == 0 {
		return nil
	}
	idx := rand.Intn(len(instances))

	return &instances[idx]
}
