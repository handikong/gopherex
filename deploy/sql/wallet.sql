-- 建议：单独建库
-- CREATE DATABASE order_service_db CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
-- USE order_service_db;

-- 订单主表：订单状态机（NEW / PARTIAL / FILLED / CANCELED / REJECTED）
CREATE TABLE IF NOT EXISTS orders (
  order_id      BIGINT UNSIGNED NOT NULL COMMENT '订单ID（雪花/自增/发号器均可），全局唯一',
  idem_key      VARCHAR(128) NOT NULL COMMENT '下单幂等键（Idempotency-Key 或 client_order_id），同一键只产生一个订单',
  user_id       BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  symbol        VARCHAR(16) NOT NULL COMMENT '交易对，例如 BTCUSDT / ETHUSDT',
  side          TINYINT UNSIGNED NOT NULL COMMENT '方向：1=BUY 2=SELL',
  ord_type      TINYINT UNSIGNED NOT NULL COMMENT '订单类型：1=LIMIT 2=MARKET',
  tif           TINYINT UNSIGNED NOT NULL COMMENT '有效方式：1=GTC 2=IOC 3=FOK',
  price_ticks   BIGINT NOT NULL COMMENT '限价单价格（tick为最小单位）；市价单可填0',
  qty_lots      BIGINT UNSIGNED NOT NULL COMMENT '下单数量（lot为最小单位）',
  remaining_lots BIGINT UNSIGNED NOT NULL COMMENT '剩余未成交数量（lot为最小单位）',
  status        TINYINT UNSIGNED NOT NULL COMMENT '状态：1=NEW 2=PARTIAL 3=FILLED 4=CANCELED 5=REJECTED',
  reject_code   INT NOT NULL DEFAULT 0 COMMENT '拒单码（0=未拒单）；用于可观测与前端提示',
  created_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  updated_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) COMMENT '更新时间',
  PRIMARY KEY (order_id),
  UNIQUE KEY uk_orders_idem (idem_key),
  KEY idx_orders_user_time (user_id, created_at),
  KEY idx_orders_symbol_time (symbol, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='订单主表：记录下单请求与订单状态机';

-- 下单幂等结果缓存表：存“第一次成功响应”
-- 好处：重试时直接返回同一份 response（可包含order_id/状态/提示等）
CREATE TABLE IF NOT EXISTS order_idempotency (
  idem_key    VARCHAR(128) NOT NULL COMMENT '幂等键（请求唯一标识）',
  order_id    BIGINT UNSIGNED NOT NULL COMMENT '对应订单ID',
  http_code   INT NOT NULL COMMENT '第一次响应的HTTP状态码（如200/400）',
  resp_json   JSON NOT NULL COMMENT '第一次响应体（可包含order_id、reject_code等）',
  created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  PRIMARY KEY (idem_key),
  KEY idx_idem_order (order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='下单幂等结果：同一 idem_key 重试直接返回第一次结果';

-- （可选但强烈建议）Outbox：订单服务对外发布“订单状态变化事件”
-- 用于可靠通知下游（WS、用户中心、审计等），避免“DB写成功但消息没发”
CREATE TABLE IF NOT EXISTS order_outbox (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键（用于顺序扫描）',
  event_id     VARCHAR(64) NOT NULL COMMENT '事件ID（建议=order_id:version 或 hash）',
  event_type   VARCHAR(32) NOT NULL COMMENT '事件类型：ORDER_CREATED/ORDER_UPDATED/ORDER_REJECTED 等',
  aggregate_id BIGINT UNSIGNED NOT NULL COMMENT '聚合根ID（这里是 order_id）',
  payload      JSON NOT NULL COMMENT '事件载荷（给下游用）',
  status       TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '状态：1=PENDING 2=SENT 3=FAILED',
  created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  sent_at      TIMESTAMP(6) NULL DEFAULT NULL COMMENT '发送时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_outbox_event (event_id),
  KEY idx_outbox_status_id (status, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='订单Outbox：可靠发布订单事件（配合publisher轮询/游标）';



