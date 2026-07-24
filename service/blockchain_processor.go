package service

import (
	"context"
	"crynux_as/blockchain"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"gorm.io/gorm"

	log "github.com/sirupsen/logrus"
)

// StartBlockchainProcessors starts one deposit processing worker per configured
// blockchain network. Each worker scans ERC20 Transfer logs of the configured
// token contracts whose receiver is the platform receiving address, and turns
// them into deposits and Credits ledger events.
func StartBlockchainProcessors(ctx context.Context) {
	appConfig := config.GetConfig()
	for network := range appConfig.Blockchains {
		go func(network string) {
			if err := runBlockchainProcessor(ctx, config.GetDB(), network); err != nil {
				log.Errorf("blockchain processor for network %s stopped with error: %v", network, err)
			}
		}(network)
	}
}

func runBlockchainProcessor(ctx context.Context, db *gorm.DB, network string) error {
	appConfig := config.GetConfig()
	networkConfig, ok := appConfig.Blockchains[network]
	if !ok {
		return fmt.Errorf("blockchain network %s not found in config", network)
	}

	client, err := blockchain.GetBlockchainClient(network)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(time.Duration(networkConfig.ScanInterval) * time.Second)
	defer ticker.Stop()

	log.Infof("blockchain processor for network %s started, scan_interval=%ds", network, networkConfig.ScanInterval)

	for {
		select {
		case <-ctx.Done():
			log.Infof("blockchain processor for network %s stopped", network)
			return ctx.Err()
		case <-ticker.C:
			if err := processNetworkBlockRange(ctx, db, client, networkConfig); err != nil {
				log.Errorf("failed to process blockchain range on %s: %v", network, err)
			}
		}
	}
}

func processNetworkBlockRange(ctx context.Context, db *gorm.DB, client *blockchain.BlockchainClient, networkConfig config.BlockchainNetworkConfig) error {
	latestBlockNum, err := client.GetLatestBlockNum(ctx)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}

	cursor, err := models.GetBlockchainCursor(ctx, db, client.Network, networkConfig.StartBlockNum)
	if err != nil {
		return fmt.Errorf("get blockchain cursor: %w", err)
	}

	if cursor.LastBlockNum >= latestBlockNum {
		return nil
	}

	startBlock := cursor.LastBlockNum + 1
	endBlock := latestBlockNum
	if endBlock-startBlock+1 > networkConfig.LogBlockRange {
		endBlock = startBlock + networkConfig.LogBlockRange - 1
	}

	if err := processERC20DepositLogs(ctx, db, client, networkConfig, startBlock, endBlock); err != nil {
		return err
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.WithContext(dbCtx).Model(cursor).Updates(map[string]interface{}{
		"last_block_num":   endBlock,
		"last_update_time": time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("update blockchain cursor: %w", err)
	}

	return nil
}

func processERC20DepositLogs(ctx context.Context, db *gorm.DB, client *blockchain.BlockchainClient, networkConfig config.BlockchainNetworkConfig, startBlock, endBlock uint64) error {
	tokenAddresses := make([]common.Address, 0, len(networkConfig.Tokens))
	tokenByAddress := make(map[string]string, len(networkConfig.Tokens))
	for name, tokenConfig := range networkConfig.Tokens {
		addr := common.HexToAddress(tokenConfig.Address)
		tokenAddresses = append(tokenAddresses, addr)
		tokenByAddress[strings.ToLower(addr.Hex())] = name
	}

	logs, err := client.FilterERC20TransferLogs(ctx, tokenAddresses, startBlock, endBlock)
	if err != nil {
		return fmt.Errorf("filter ERC20 transfer logs: %w", err)
	}

	if len(logs) > 0 {
		log.Infof("ERC20 deposit log scan on %s from block %d to %d: %d log(s)", client.Network, startBlock, endBlock, len(logs))
	}

	for _, receiptLog := range logs {
		if err := processERC20DepositLog(ctx, db, client, networkConfig, tokenByAddress, receiptLog); err != nil {
			return err
		}
	}
	return nil
}

func processERC20DepositLog(ctx context.Context, db *gorm.DB, client *blockchain.BlockchainClient, networkConfig config.BlockchainNetworkConfig, tokenByAddress map[string]string, receiptLog types.Log) error {
	tokenName, ok := tokenByAddress[strings.ToLower(receiptLog.Address.Hex())]
	if !ok {
		log.Warnf("ignoring ERC20 transfer from unknown token contract on %s, tx: %s, log_index: %d, contract: %s",
			client.Network, receiptLog.TxHash.Hex(), receiptLog.Index, receiptLog.Address.Hex())
		return nil
	}

	fromAddress, amount, ok := parseERC20TransferLog(client.Network, receiptLog)
	if !ok {
		return nil
	}

	tokenConfig := networkConfig.Tokens[tokenName]
	credits, err := ConvertTokenAmountToCredits(amount, tokenConfig.Decimals, tokenConfig.CreditsPerToken)
	if err != nil {
		return fmt.Errorf("convert token amount to credits: %w", err)
	}

	normalizedFrom := common.HexToAddress(fromAddress).Hex()
	user, err := getUserByAddress(ctx, db, normalizedFrom)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf("ignoring deposit from unknown wallet on %s, tx: %s, log_index: %d, from: %s, token: %s, amount: %s",
				client.Network, receiptLog.TxHash.Hex(), receiptLog.Index, normalizedFrom, tokenName, amount.String())
			return nil
		}
		return err
	}

	if err := ProcessDeposit(ctx, db, ProcessDepositInput{
		UserID:      user.ID,
		Network:     client.Network,
		Token:       tokenName,
		TxHash:      receiptLog.TxHash.Hex(),
		LogIndex:    uint(receiptLog.Index),
		FromAddress: normalizedFrom,
		Amount:      amount,
		Credits:     credits,
	}); err != nil {
		return fmt.Errorf("process deposit: %w", err)
	}

	log.Infof("processed deposit on %s, tx: %s, log_index: %d, from: %s, token: %s, amount: %s, credits: %s, user_id: %d",
		client.Network, receiptLog.TxHash.Hex(), receiptLog.Index, normalizedFrom, tokenName, amount.String(), credits.String(), user.ID)
	return nil
}

func parseERC20TransferLog(network string, receiptLog types.Log) (string, *big.Int, bool) {
	if len(receiptLog.Topics) != 3 || receiptLog.Topics[0] != blockchain.ERC20TransferTopic {
		log.Warnf("ignoring unmatched ERC20 transfer log on %s, tx: %s, log_index: %d, topics: %d",
			network, receiptLog.TxHash.Hex(), receiptLog.Index, len(receiptLog.Topics))
		return "", nil, false
	}

	amount := new(big.Int).SetBytes(receiptLog.Data)
	if amount.Sign() <= 0 {
		log.Warnf("ignoring zero-amount ERC20 transfer on %s, tx: %s, log_index: %d",
			network, receiptLog.TxHash.Hex(), receiptLog.Index)
		return "", nil, false
	}

	fromAddress := common.BytesToAddress(receiptLog.Topics[1].Bytes()).Hex()
	if fromAddress == (common.Address{}).Hex() {
		log.Warnf("ignoring ERC20 transfer with zero from address on %s, tx: %s, log_index: %d, amount: %s",
			network, receiptLog.TxHash.Hex(), receiptLog.Index, amount.String())
		return "", nil, false
	}

	return fromAddress, amount, true
}

func getUserByAddress(ctx context.Context, db *gorm.DB, address string) (*models.User, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	if err := db.WithContext(dbCtx).Where("address = ?", address).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
