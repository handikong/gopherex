package monitor_test

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"gopherex.com/apps/wallet/internal/monitor"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/safe"
)

func TestMain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger.Init("test", "info")
	wssUrl := "wss://mainnet.infura.io/ws/v3/3b3402ed33804bc28c87b29fd1152c0c"
	realTimeMonitor := monitor.NewEthMonitor(wssUrl)

	safe.Go(func() {
		realTimeMonitor.Start(ctx)
	})
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info(ctx, "Server exiting...")

}
