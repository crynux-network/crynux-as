package purchase

import (
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"sort"

	"github.com/gin-gonic/gin"
)

type TokenData struct {
	Name            string `json:"name" description:"Token name key from configuration"`
	Address         string `json:"address" description:"ERC20 contract address"`
	Decimals        uint8  `json:"decimals" description:"Token decimals"`
	CreditsPerToken uint64 `json:"credits_per_token" description:"Credits awarded for one whole token"`
}

type NetworkData struct {
	Name             string      `json:"name" description:"Blockchain network name key from configuration"`
	ChainID          uint64      `json:"chain_id" description:"EVM chain ID"`
	ReceivingAddress string      `json:"receiving_address" description:"Platform address that receives ERC20 payments for Credits purchases"`
	Tokens           []TokenData `json:"tokens" description:"Supported ERC20 tokens on this network"`
}

type GetNetworksData struct {
	Networks []NetworkData `json:"networks" description:"Configured purchase networks and tokens"`
}

type GetNetworksResponse struct {
	response.Response
	Data *GetNetworksData `json:"data"`
}

func GetNetworks(c *gin.Context) (*GetNetworksResponse, error) {
	blockchains := config.GetConfig().Blockchains

	networkNames := make([]string, 0, len(blockchains))
	for name := range blockchains {
		networkNames = append(networkNames, name)
	}
	sort.Strings(networkNames)

	networks := make([]NetworkData, 0, len(networkNames))
	for _, name := range networkNames {
		netCfg := blockchains[name]

		tokenNames := make([]string, 0, len(netCfg.Tokens))
		for tokenName := range netCfg.Tokens {
			tokenNames = append(tokenNames, tokenName)
		}
		sort.Strings(tokenNames)

		tokens := make([]TokenData, 0, len(tokenNames))
		for _, tokenName := range tokenNames {
			tokenCfg := netCfg.Tokens[tokenName]
			tokens = append(tokens, TokenData{
				Name:            tokenName,
				Address:         tokenCfg.Address,
				Decimals:        tokenCfg.Decimals,
				CreditsPerToken: tokenCfg.CreditsPerToken,
			})
		}

		networks = append(networks, NetworkData{
			Name:             name,
			ChainID:          netCfg.ChainID,
			ReceivingAddress: netCfg.ReceivingAddress,
			Tokens:           tokens,
		})
	}

	return &GetNetworksResponse{
		Data: &GetNetworksData{
			Networks: networks,
		},
	}, nil
}
