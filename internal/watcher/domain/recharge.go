package domain

import "time"

// 充值的配置
type RechargeConfig struct {
	Chain           string        // "BTC" "ETH"
	Interval        time.Duration //间隔扫描Scaner
	ConfirmNum      int64         //  确认数量
	StepBlock       uint8         // 每次跳跃多少个区块  一起扫描
	ConfirmInterval time.Duration // 间隔扫码确定充值
	ConsumerCount   uint8         // 多少个消费者
	ScanMode        string        //扫码方式
}
