package blockchain

import (
	"context"
	"crynux_as/config"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/time/rate"
)

// ERC20TransferTopic is the topic hash of the ERC20 Transfer(address,address,uint256) event
var ERC20TransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type BlockchainClient struct {
	Network          string
	RpcClient        *ethclient.Client
	RpcEndpoint      string
	ChainID          *big.Int
	StartBlockNum    uint64
	LogBlockRange    uint64
	ReceivingAddress common.Address
	Tokens           map[string]config.TokenConfig
	Limiter          *rate.Limiter
}

var blockchainClients = make(map[string]*BlockchainClient)
var ErrBlockchainNotFound = errors.New("blockchain not found")

func GetBlockchainClient(network string) (*BlockchainClient, error) {
	client, exists := blockchainClients[network]
	if !exists {
		return nil, ErrBlockchainNotFound
	}
	return client, nil
}

func initBlockchainClient(ctx context.Context, network string) error {
	appConfig := config.GetConfig()
	blockchain, exists := appConfig.Blockchains[network]
	if !exists {
		return ErrBlockchainNotFound
	}

	client, err := ethclient.Dial(blockchain.RpcEndpoint)
	if err != nil {
		return err
	}

	chainID, err := initChainID(ctx, client, blockchain.ChainID)
	if err != nil {
		return err
	}

	limiter := rate.NewLimiter(rate.Limit(blockchain.RPS), int(blockchain.RPS))

	blockchainClients[network] = &BlockchainClient{
		Network:          network,
		RpcClient:        client,
		RpcEndpoint:      blockchain.RpcEndpoint,
		ChainID:          chainID,
		StartBlockNum:    blockchain.StartBlockNum,
		LogBlockRange:    blockchain.LogBlockRange,
		ReceivingAddress: common.HexToAddress(blockchain.ReceivingAddress),
		Tokens:           blockchain.Tokens,
		Limiter:          limiter,
	}
	return nil
}

func Init(ctx context.Context) error {
	appConfig := config.GetConfig()
	for network := range appConfig.Blockchains {
		if err := initBlockchainClient(ctx, network); err != nil {
			return err
		}
	}
	return nil
}

func initChainID(ctx context.Context, client *ethclient.Client, chainIDNum uint64) (*big.Int, error) {
	var chainID *big.Int
	if chainIDNum > 0 {
		chainID = big.NewInt(0).SetUint64(chainIDNum)
	} else {
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		id, err := client.ChainID(callCtx)
		if err != nil {
			return nil, err
		}
		chainID = id
	}
	return chainID, nil
}

// GetLatestBlockNum returns the latest block number of the network
func (client *BlockchainClient) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	if err := client.Limiter.Wait(ctx); err != nil {
		return 0, err
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return client.RpcClient.BlockNumber(callCtx)
}

// FilterERC20TransferLogs returns the ERC20 Transfer logs of the given token contracts
// whose `to` address is the receiving address, within [fromBlock, toBlock]
func (client *BlockchainClient) FilterERC20TransferLogs(ctx context.Context, tokenAddresses []common.Address, fromBlock, toBlock uint64) ([]types.Log, error) {
	if err := client.Limiter.Wait(ctx); err != nil {
		return nil, err
	}

	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(0).SetUint64(fromBlock),
		ToBlock:   big.NewInt(0).SetUint64(toBlock),
		Addresses: tokenAddresses,
		Topics: [][]common.Hash{
			{ERC20TransferTopic},
			nil,
			{common.BytesToHash(client.ReceivingAddress.Bytes())},
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return client.RpcClient.FilterLogs(callCtx, query)
}
