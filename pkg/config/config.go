package config

import (
	"log"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

func LoadAndWatch(service string, out interface{}) (*viper.Viper, error) {
	v := viper.New()
	// 约定：config/{service}.yaml
	v.SetConfigName(service)
	v.SetConfigType("yaml")
	v.AddConfigPath("./config")
	v.AddConfigPath(".") // 兜底，直接放当前目录也行

	// 环境变量覆盖，例如：
	//   GATEWAY_HTTP_ADDR 覆盖 http.addr
	//   GATEWAY_ETCD_ENDPOINTS 覆盖 etcd.endpoints
	v.SetEnvPrefix(strings.ToUpper(service))
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	if err := v.Unmarshal(out); err != nil {
		return nil, err
	}

	log.Printf("[%s] config loaded from %s", service, v.ConfigFileUsed())

	// 监听文件变更，热更新到 out
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		log.Printf("[%s] config file changed: %s", service, e.Name)

		if err := v.Unmarshal(out); err != nil {
			log.Printf("[%s] reload config error: %v", service, err)
			return
		}
		log.Printf("[%s] config reloaded OK", service)
	})

	return v, nil
}
