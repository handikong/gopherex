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

-- ==========================================
-- 3. [新增] 用户充值地址表 (Day 15 核心)
-- 记录每个用户在每条链上的专属充值地址
-- ==========================================
CREATE TABLE IF NOT EXISTS `user_addresses` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `address` varchar(100) NOT NULL COMMENT '生成的充值地址',
  `pkh_idx` int NOT NULL COMMENT 'HD钱包路径索引 (通常=UserID)',
  `created_at` timestamp DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  -- 保证每个用户在每条链只有一个地址
  UNIQUE KEY `uniq_user_chain` (`user_id`, `chain`),
  -- 保证地址不重复分配
  UNIQUE KEY `uniq_address` (`address`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户充值地址表';

-- ==========================================
-- 4. [新增] 用户资产表 (核心账本)
-- 记录用户持有的每种币的余额
-- ==========================================
CREATE TABLE IF NOT EXISTS `user_assets` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `coin_symbol` varchar(20) NOT NULL COMMENT '币种: BTC, ETH, USDT',
  `available` decimal(36,18) NOT NULL DEFAULT '0.000000000000000000' COMMENT '可用余额',
  `frozen` decimal(36,18) NOT NULL DEFAULT '0.000000000000000000' COMMENT '冻结余额(下单/提现冻结)',
  `version` bigint NOT NULL DEFAULT 0 COMMENT '乐观锁版本号',
  `created_at` timestamp DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  -- 一个用户一种币只有一行记录
  UNIQUE KEY `uniq_user_coin` (`user_id`, `coin_symbol`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户资产表';

-- 5. [新增] 提现记录表 (Day 17 核心)
-- 跟踪每一笔提现的状态 
-- ========================================== 
CREATE TABLE IF NOT EXISTS `withdraws` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `symbol` varchar(20) NOT NULL COMMENT '币种: BTC, USDT',
  `amount` decimal(36,18) NOT NULL COMMENT '提现金额',
  `fee` decimal(36,18) NOT NULL COMMENT '提现手续费',
  `to_address` varchar(100) NOT NULL COMMENT '提现到账地址',
  `tx_hash` varchar(100) NOT NULL DEFAULT '' COMMENT '链上交易Hash',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0:Applying(申请中), 1:Audited(审核通过), 2:Processing(广播中), 3:Confirmed(已确认), 4:Failed(失败), 5:Rejected(驳回)',
  `error_msg` varchar(255) NOT NULL DEFAULT '' COMMENT '失败或驳回原因',
  `created_at` timestamp DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status` (`user_id`, `status`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='提现记录表';