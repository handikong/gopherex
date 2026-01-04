package main

// 区块的扫描 使用go-ethereum

//var url = "wss://mainnet.infura.io/ws/v3/3b3402ed33804bc28c87b29fd1152c0c"
//var transferEventSignatureHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
//
//// ERC-20 标准 ABI 部分：name() 和 symbol() 函数
//var erc20ABI = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},
//                 {"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},
//                 {"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"}]`
//
//func main() {
//
//	// 使用go-ethereum 监听链上的数据
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	// 随机种子 为了随机数
//	rand.NewSource(time.Now().UnixNano())
//	clinet, err := ethclient.DialContext(ctx, url)
//	if err != nil {
//		log.Fatalf("failed to connect to Ethereum client: %v", err)
//	}
//	// 启动一个链上的扫描
//	scaner(ctx, clinet)
//}
//
//func scaner(ctx context.Context, c *ethclient.Client) {
//	// 假设当前的区块为100
//	number, err := c.BlockNumber(ctx)
//	if err != nil {
//		log.Fatalf("failed to get block number: %v", err)
//	}
//	prev := number - 1
//	newInt := big.NewInt(int64(prev))
//	currentBlock, err := c.BlockByNumber(ctx, newInt)
//	if err != nil {
//		log.Printf("failed to get block by number: %v", err)
//		return
//	}
//
//	// 循环交易信息
//	if len(currentBlock.Transactions()) > 0 {
//		for _, tx := range currentBlock.Transactions() {
//			txHash := tx.Hash()
//
//			//ethAmount := transactionValueToEth(tx.Value())
//			//fmt.Printf("ETH Transfer Amount: %f ETH\n", ethAmount)
//
//			// 打印交易详细信息
//			//fmt.Printf("Transaction Hash: %s\n", txHash.Hex())
//			//fmt.Printf("Transaction Value: %s\n", transaction.Value().String())
//			//fmt.Printf("Pending: %v\n", pending)
//			// 获取交易收据
//			receipt, err := c.TransactionReceipt(context.Background(), txHash)
//			if err != nil {
//				log.Printf("Failed to get transaction receipt: %v", err)
//				continue
//			}
//			// 2. ERC-20 代币转账金额（如果有）
//			for _, txLog := range receipt.Logs {
//				// Transfer 事件的主题（Hash）
//				if len(txLog.Topics) >= 3 && txLog.Topics[0] == transferEventSignatureHash {
//					from := common.HexToAddress(txLog.Topics[1].Hex())
//					to := common.HexToAddress(txLog.Topics[2].Hex())
//					fmt.Printf("data is s %s \n", string(txLog.Data))
//					value := new(big.Int)
//					value.SetBytes(txLog.Data) // 转账金额
//					fmt.Printf("value is %s \n", value.String())
//					// 获取代币的名称和 decimals
//					tokenAddress := txLog.Address // ERC-20 合约地址（例如 USDT）
//					fmt.Printf("tokenAddress is %s \n", tokenAddress.Hex())
//					tokenInfo, err := getTokenInfo(c, tokenAddress)
//					if err != nil {
//						log.Printf("Failed to get token info: %v", err)
//						continue
//					}
//
//					// 转换为 ETH 单位
//					tokenAmountInEth := convertTokenToEth(value, int(tokenInfo.Decimals))
//					fmt.Printf("ERC-20 hash is %s Transfer from %s to %s, Value: %s,address :%s, ETH (converted from %s %+v)\n", tx.Hash(), from.Hex(), to.Hex(), tokenAmountInEth.String(), value.String(), tokenAddress.Hex(), tokenInfo)
//				}
//			}
//
//		}
//	}
//	//fmt.Printf("current block number: %+v\n", currentBlock)
//	//// 定义结构体，用于序列化区块的关键信息
//	//blockInfo := struct {
//	//	Number            *big.Int `json:"number"`
//	//	Hash              string   `json:"hash"`
//	//	ParentHash        string   `json:"parentHash"`
//	//	Nonce             uint64   `json:"nonce"`
//	//	GasLimit          uint64   `json:"gasLimit"`
//	//	GasUsed           uint64   `json:"gasUsed"`
//	//	Transactions      []string `json:"transactions"`
//	//	TransactionsCount int      `json:"transactionsCount"`
//	//	Timestamp         uint64   `json:"timestamp"`
//	//}{
//	//	Number:            currentBlock.Number(),
//	//	Hash:              currentBlock.Hash().Hex(),
//	//	ParentHash:        currentBlock.ParentHash().Hex(),
//	//	Nonce:             currentBlock.Nonce(),
//	//	GasLimit:          currentBlock.GasLimit(),
//	//	GasUsed:           currentBlock.GasUsed(),
//	//	TransactionsCount: len(currentBlock.Transactions()),
//	//	Timestamp:         currentBlock.Time(),
//	//}
//	//
//	//// 获取区块中的每个交易的哈希
//	//for _, tx := range currentBlock.Transactions() {
//	//	blockInfo.Transactions = append(blockInfo.Transactions, tx.Hash().Hex())
//	//}
//	//
//	//// 将结构体序列化为 JSON
//	//blockData, err := json.MarshalIndent(blockInfo, "", "  ")
//	//if err != nil {
//	//	log.Printf("failed to marshal block: %v", err)
//	//	return
//	//}
//	//
//	//// 打印区块的所有信息
//	//fmt.Printf("Full block info:\n%s\n", string(blockData))
//
//	//printBlock(currentBlock)
//
//}
//
//// 代币信息结构体
//type TokenInfo struct {
//	Name     string
//	Symbol   string
//	Decimals uint8
//}
//
//// 获取 ERC-20 代币的名称、符号和小数位数
//func getTokenInfo(client *ethclient.Client, tokenAddress common.Address) (TokenInfo, error) {
//	// 如果缓存没有，动态获取代币信息
//
//	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to parse ERC-20 ABI: %v", err)
//	}
//	pack, _ := parsedABI.Pack("name")
//	// 获取代币名称
//	nameData, err := client.CallContract(context.Background(), ethereum.CallMsg{
//		To:   &tokenAddress,
//		Data: pack,
//	}, nil)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to get token name: %v", err)
//	}
//	var tokenName string
//	err = parsedABI.UnpackIntoInterface(&tokenName, "name", nameData)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to unpack token name: %v", err)
//	}
//	fmt.Printf("tokenname is %s \n", tokenName)
//	symbol, _ := parsedABI.Pack("symbol")
//	// 获取代币符号
//	symbolData, err := client.CallContract(context.Background(), ethereum.CallMsg{
//		To:   &tokenAddress,
//		Data: symbol,
//	}, nil)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to get token symbol: %v", err)
//	}
//	var tokenSymbol string
//	err = parsedABI.UnpackIntoInterface(&tokenSymbol, "symbol", symbolData)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to unpack token symbol: %v", err)
//	}
//	dc, _ := parsedABI.Pack("decimals")
//	// 获取代币的 decimals
//	decimalsData, err := client.CallContract(context.Background(), ethereum.CallMsg{
//		To:   &tokenAddress,
//		Data: dc,
//	}, nil)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to get token decimals: %v", err)
//	}
//	var tokenDecimals uint8
//	err = parsedABI.UnpackIntoInterface(&tokenDecimals, "decimals", decimalsData)
//	if err != nil {
//		return TokenInfo{}, fmt.Errorf("failed to unpack token decimals: %v", err)
//	}
//
//	// 将代币信息存入缓存
//	tokenInfo := TokenInfo{
//		Name:     tokenName,
//		Symbol:   tokenSymbol,
//		Decimals: tokenDecimals,
//	}
//
//	return tokenInfo, nil
//}
//
//// 将 ETH 转账金额从 Wei 转换为 ETH
//func transactionValueToEth(valueInWei *big.Int) *big.Float {
//	ethValue := new(big.Float).Quo(new(big.Float).SetInt(valueInWei), big.NewFloat(1e18)) // 1 ETH = 10^18 Wei
//	return ethValue
//}
//
//// 将 ERC-20 代币数量转换为 ETH 单位
//func convertTokenToEth(tokenAmount *big.Int, decimals int) *big.Float {
//	// 代币的最小单位：10^decimals
//	decimalFactor := new(big.Float).SetInt(big.NewInt(1).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
//	ethAmount := new(big.Float).Quo(new(big.Float).SetInt(tokenAmount), decimalFactor)
//	return ethAmount
//}

// 0x7149874588a6753352dd019Dce8bde034D130125

// 0x4620e7d0dac58e49dd78e3c66feabc00a6a80a77
