package blockchain

import (
	"context"
	"crynux_as/config"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const benefitAddressABIJSON = `[{"inputs":[{"internalType":"address","name":"nodeAddress","type":"address"}],"name":"getBenefitAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"}]`

// GetBenefitAddress resolves the on-chain Beneficial Address for a login wallet on the given network.
// When the contract returns the zero address, the login wallet itself is used.
func GetBenefitAddress(ctx context.Context, owner common.Address, network string) (common.Address, error) {
	cfg := config.GetConfig()
	networkCfg, ok := cfg.Provider.Wallet.Networks[network]
	if !ok {
		return common.Address{}, fmt.Errorf("withdrawal network %s is not configured", network)
	}
	client, err := GetBlockchainClient(network)
	if err != nil {
		return common.Address{}, err
	}
	parsedABI, err := abi.JSON(strings.NewReader(benefitAddressABIJSON))
	if err != nil {
		return common.Address{}, err
	}
	data, err := parsedABI.Pack("getBenefitAddress", owner)
	if err != nil {
		return common.Address{}, err
	}
	contract := common.HexToAddress(networkCfg.BenefitAddressContract)
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.Limiter.Wait(callCtx); err != nil {
		return common.Address{}, err
	}
	out, err := client.RpcClient.CallContract(callCtx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, err
	}
	values, err := parsedABI.Unpack("getBenefitAddress", out)
	if err != nil {
		return common.Address{}, err
	}
	if len(values) != 1 {
		return common.Address{}, fmt.Errorf("unexpected getBenefitAddress result length")
	}
	benefit, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected getBenefitAddress result type")
	}
	if benefit == (common.Address{}) {
		return owner, nil
	}
	return benefit, nil
}

// ParseCNXToWei converts a non-negative CNX decimal string with at most 18 fractional digits to wei.
func ParseCNXToWei(value string) (*big.Int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("empty CNX amount")
	}
	rat := new(big.Rat)
	if _, ok := rat.SetString(trimmed); !ok {
		return nil, fmt.Errorf("invalid CNX amount")
	}
	if rat.Sign() < 0 {
		return nil, fmt.Errorf("CNX amount must be non-negative")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	scaled := new(big.Rat).Mul(rat, new(big.Rat).SetInt(scale))
	if !scaled.IsInt() {
		return nil, fmt.Errorf("CNX amount has more than 18 fractional digits")
	}
	return new(big.Int).Set(scaled.Num()), nil
}
