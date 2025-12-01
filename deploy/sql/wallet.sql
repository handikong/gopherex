CREATE DATABASE IF NOT EXISTS gopherex_wallet;
USE gopherex_wallet;

-- 1. 充值记录表 (核心表)
-- 用于记录每一笔从链上扫到的充值
CREATE TABLE `deposits` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `symbol` varchar(20) NOT NULL COMMENT '币种: BTC, USDT, ETH',
  `tx_hash` varchar(100) NOT NULL COMMENT '交易哈希',
  `log_index` int NOT NULL DEFAULT 0 COMMENT '日志索引(BTC固定0, ETH Log有索引)',
  `from_address` varchar(100) NOT NULL DEFAULT '' COMMENT '发送方',
  `to_address` varchar(100) NOT NULL COMMENT '接收方(我们的充值地址)',
  `amount` decimal(36,18) NOT NULL COMMENT '金额(高精度)',
  `block_height` bigint NOT NULL COMMENT '区块高度',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0:Pending(确认中), 1:Confirmed(已入账)',
  `error_msg` varchar(255) NOT NULL DEFAULT '' COMMENT '错误信息',
  `created_at` timestamp DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  -- 🔥 核心唯一索引：防止重复入账 (幂等性保证)
  UNIQUE KEY `uniq_tx` (`chain`, `tx_hash`, `log_index`),
  KEY `idx_address` (`to_address`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='充值记录表';

-- 2. 扫描游标表 (断点续传)
-- 记录每个链扫到了哪里，防止重启后重头开始扫
CREATE TABLE `scans` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `current_height` bigint NOT NULL COMMENT '当前已处理的高度',
  `current_hash` varchar(100) NOT NULL DEFAULT '' COMMENT '当前块Hash(用于防分叉回滚)',
  `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_chain` (`chain`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='区块扫描游标表';

-- 3. 初始化游标数据 (重要！)
-- 如果不插这条数据，Scanner 启动时查不到记录可能会报错或者从0开始
INSERT INTO `scans` (`chain`, `current_height`, `current_hash`) 
VALUES 
('BTC', 0, ''),
('ETH', 0, '')
ON DUPLICATE KEY UPDATE chain=chain;