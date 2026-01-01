CREATE TABLE IF NOT EXISTS balances (
  owner_type  TINYINT UNSIGNED NOT NULL COMMENT '账户所有者类型：1=USER 2=SYSTEM',
  owner_id    BIGINT UNSIGNED NOT NULL COMMENT '所有者ID：user_id；SYSTEM可用0或固定ID',
  asset       VARCHAR(16) NOT NULL COMMENT '资产：BTC/ETH/USDT...',
  bucket      VARCHAR(32) NOT NULL COMMENT '余额桶：spot_available/spot_frozen/system_fee/...',
  amount      BIGINT NOT NULL COMMENT '余额（最小单位，允许为0；一般不允许为负，除非设计允许）',
  updated_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) COMMENT '更新时间',
  PRIMARY KEY (owner_type, owner_id, asset, bucket),
  KEY idx_bal_owner_asset (owner_type, owner_id, asset)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='余额快照：bucket 化余额（读优化，可由ledger重建）';

-- EntrySet：一笔“资金动作”的原子单位（幂等单位）
-- 同一个 idempotency_key 只能成功一次（重复则直接返回已处理）
CREATE TABLE IF NOT EXISTS ledger_entrysets (
  entryset_id      CHAR(36) NOT NULL COMMENT 'EntrySet ID（UUIDv7/uuid，业务动作唯一）',
  idempotency_key  VARCHAR(128) NOT NULL COMMENT '幂等键：fill_id / place_order_idem / withdraw_id / txid:idx 等',
  es_type          VARCHAR(32) NOT NULL COMMENT '类型：RESERVE/RELEASE/SETTLE/DEPOSIT/WITHDRAW...',
  ref_id           VARCHAR(128) NOT NULL COMMENT '外部引用ID：order_id/fill_id/withdraw_id/txid...',
  status           TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '状态：1=APPLIED（预留扩展：0=INIT/2=VOID等）',
  created_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  PRIMARY KEY (entryset_id),
  UNIQUE KEY uk_es_idem (idempotency_key),
  KEY idx_es_ref (ref_id),
  KEY idx_es_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='账本EntrySet：资金动作批次（幂等与审计入口）';

-- Ledger Entries：分录（append-only 真相）
-- 同一个 entryset_id 里，按 asset 做 delta 求和必须为0（业务层保证）
CREATE TABLE IF NOT EXISTS ledger_entries (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '分录ID（自增，仅用于索引与顺序）',
  entryset_id   CHAR(36) NOT NULL COMMENT '所属EntrySet',
  owner_type    TINYINT UNSIGNED NOT NULL COMMENT '1=USER 2=SYSTEM',
  owner_id      BIGINT UNSIGNED NOT NULL COMMENT '用户ID或系统ID',
  asset         VARCHAR(16) NOT NULL COMMENT '资产：BTC/ETH/USDT...',
  bucket        VARCHAR(32) NOT NULL COMMENT '余额桶：spot_available/spot_frozen/system_fee/...',
  delta         BIGINT NOT NULL COMMENT '变化量（最小单位，有正负；正=增加，负=减少）',
  reason        VARCHAR(32) NOT NULL COMMENT '原因：FREEZE/UNFREEZE/TRADE/FEE/DEPOSIT/WITHDRAW...',
  created_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  PRIMARY KEY (id),
  KEY idx_le_es (entryset_id),
  KEY idx_le_owner_asset_id (owner_type, owner_id, asset, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='账本分录：append-only；Balance可由此重建与对账';

-- （强烈建议）Fills 幂等表：确保同一 fill_id 只结算一次
-- 也可以只依赖 ledger_entrysets.uk_es_idem（idempotency_key=fill_id），这里给显式表更清晰
CREATE TABLE IF NOT EXISTS settled_fills (
  fill_id      VARCHAR(64) NOT NULL COMMENT '成交ID（撮合产生，全局唯一）',
  symbol       VARCHAR(16) NOT NULL COMMENT '交易对',
  created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '首次结算时间',
  PRIMARY KEY (fill_id),
  KEY idx_fill_symbol (symbol, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='已结算成交幂等表：防止重复结算（可选，uk_es_idem 也能做）';

-- （可选但强烈建议）Outbox：资金服务对外发布 BalanceChanged / LedgerAppended
CREATE TABLE IF NOT EXISTS funds_outbox (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键（用于顺序扫描）',
  event_id     VARCHAR(64) NOT NULL COMMENT '事件ID（建议=entryset_id 或 hash）',
  event_type   VARCHAR(32) NOT NULL COMMENT '事件类型：BALANCE_CHANGED/LEDGER_APPENDED 等',
  partition_key VARCHAR(64) NOT NULL COMMENT '分区键：user_id 或 symbol（便于下游按键有序处理）',
  payload      JSON NOT NULL COMMENT '事件载荷',
  status       TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '状态：1=PENDING 2=SENT 3=FAILED',
  created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  sent_at      TIMESTAMP(6) NULL DEFAULT NULL COMMENT '发送时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_funds_outbox_event (event_id),
  KEY idx_funds_outbox_status_id (status, id),
  KEY idx_funds_outbox_part (partition_key, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='资金Outbox：可靠发布余额/账本事件（publisher可恢复）';
