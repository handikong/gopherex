-- MySQL dump 10.13  Distrib 8.0.44, for Linux (x86_64)
--
-- Host: localhost    Database: gopherex_wallet
-- ------------------------------------------------------
-- Server version	8.0.44

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `deposits`
--

DROP TABLE IF EXISTS `deposits`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `deposits` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `symbol` varchar(20) NOT NULL COMMENT '币种: BTC, USDT, ETH',
  `tx_hash` varchar(100) NOT NULL COMMENT '交易哈希',
  `log_index` int NOT NULL DEFAULT '0' COMMENT '日志索引(BTC固定0, ETH Log有索引)',
  `from_address` varchar(100) NOT NULL DEFAULT '' COMMENT '发送方',
  `to_address` varchar(100) NOT NULL COMMENT '接收方(我们的充值地址)',
  `amount` decimal(36,18) NOT NULL COMMENT '金额(高精度)',
  `block_height` bigint NOT NULL COMMENT '区块高度',
  `status` tinyint NOT NULL DEFAULT '0' COMMENT '0:Pending(确认中), 1:Confirmed(已入账)',
  `error_msg` varchar(255) NOT NULL DEFAULT '' COMMENT '错误信息',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_tx` (`chain`,`tx_hash`,`log_index`),
  KEY `idx_address` (`to_address`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB AUTO_INCREMENT=1258 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='充值记录表';
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `deposits`
--


/*!40000 ALTER TABLE `deposits` ENABLE KEYS */;

--
-- Table structure for table `scans`
--

/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `scans` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `current_height` bigint NOT NULL COMMENT '当前已处理的高度',
  `current_hash` varchar(100) NOT NULL DEFAULT '' COMMENT '当前块Hash(用于防分叉回滚)',
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_chain` (`chain`)
) ENGINE=InnoDB AUTO_INCREMENT=2358 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='区块扫描游标表';
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `scans`
--

/*!40000 ALTER TABLE `scans` DISABLE KEYS */;

/*!40000 ALTER TABLE `scans` ENABLE KEYS */;

--
-- Table structure for table `user_addresses`
--

/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `user_addresses` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `address` varchar(100) NOT NULL COMMENT '生成的充值地址',
  `pkh_idx` int NOT NULL COMMENT 'HD钱包路径索引 (通常=UserID)',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_user_chain` (`user_id`,`chain`),
  UNIQUE KEY `uniq_address` (`address`)
) ENGINE=InnoDB AUTO_INCREMENT=3 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户充值地址表';
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `user_addresses`
--

/*!40000 ALTER TABLE `user_addresses` DISABLE KEYS */;
INSERT INTO `user_addresses` VALUES (1,1,'EHT','0x5fc8d32690cc91d4c39d9d3abcbd16989f875707',1,'2025-12-02 12:34:02'),(2,2,'BTC','bcrt1qy0vmja86vjzmk0eftqdef8ukp3xcajg6us33eu',2,'2025-12-03 08:41:22');
/*!40000 ALTER TABLE `user_addresses` ENABLE KEYS */;

--
-- Table structure for table `user_assets`
--

/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `user_assets` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `coin_symbol` varchar(20) NOT NULL COMMENT '币种: BTC, ETH, USDT',
  `available` decimal(36,18) NOT NULL DEFAULT '0.000000000000000000' COMMENT '可用余额',
  `frozen` decimal(36,18) NOT NULL DEFAULT '0.000000000000000000' COMMENT '冻结余额(下单/提现冻结)',
  `version` bigint NOT NULL DEFAULT '0' COMMENT '乐观锁版本号',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_user_coin` (`user_id`,`coin_symbol`)
) ENGINE=InnoDB AUTO_INCREMENT=18 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户资产表';
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `user_assets`
--

/*!40000 ALTER TABLE `user_assets` DISABLE KEYS */;


--
-- Table structure for table `withdraws`
--

/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `withdraws` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `chain` varchar(10) NOT NULL COMMENT '链: BTC, ETH',
  `symbol` varchar(20) NOT NULL COMMENT '币种: BTC, USDT',
  `amount` decimal(36,18) NOT NULL COMMENT '提现金额',
  `fee` decimal(36,18) NOT NULL COMMENT '提现手续费',
  `to_address` varchar(100) NOT NULL COMMENT '提现到账地址',
  `tx_hash` varchar(100) NOT NULL DEFAULT '' COMMENT '链上交易Hash',
  `status` tinyint NOT NULL DEFAULT '0' COMMENT '0:Applying(申请中), 1:Audited(审核通过), 2:Processing(广播中), 3:Confirmed(已确认), 4:Failed(失败), 5:Rejected(驳回)',
  `error_msg` varchar(255) NOT NULL DEFAULT '' COMMENT '失败或驳回原因',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status` (`user_id`,`status`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='提现记录表';
/*!40101 SET character_set_client = @saved_cs_client */;


ALTER TABLE withdraws 
ADD COLUMN request_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '幂等键/业务流水号';

-- 2. 🔥 建立唯一索引 (核心)
-- 这一步不仅防止重复，还利用 B+树 提供了极快的查询速度
CREATE UNIQUE INDEX idx_withdraw_request_id ON withdraws(request_id);

--
-- Dumping data for table `withdraws`
--


/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2025-12-04  9:36:58
