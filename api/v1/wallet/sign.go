package wallet

import (
	"crynux_as/config"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func jsonRemarshal(bytes []byte) ([]byte, error) {
	var ifce interface{}
	if err := json.Unmarshal(bytes, &ifce); err != nil {
		return nil, err
	}
	return json.Marshal(ifce)
}

func VerifyWalletSignature(business any, timestamp int64, signature string) error {
	cfg := config.GetConfig()
	now := time.Now().Unix()
	if math.Abs(float64(now-timestamp)) > 60 {
		return errors.New("timestamp out of range")
	}
	dataBytes, err := json.Marshal(business)
	if err != nil {
		return err
	}
	dataBytes, err = jsonRemarshal(dataBytes)
	if err != nil {
		return err
	}
	timestampBytes := []byte(strconv.FormatInt(timestamp, 10))
	signBytes := append(dataBytes, timestampBytes...)
	hash := crypto.Keccak256Hash(signBytes)
	sig := strings.TrimPrefix(signature, "0x")
	sigBytes, err := hexutil.Decode("0x" + sig)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	if len(sigBytes) != 65 {
		return errors.New("signature must be 65 bytes")
	}
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}
	pub, err := crypto.SigToPub(hash.Bytes(), sigBytes)
	if err != nil {
		return err
	}
	recovered := crypto.PubkeyToAddress(*pub)
	expected := common.HexToAddress(cfg.Provider.Wallet.SignerAddress)
	if !strings.EqualFold(recovered.Hex(), expected.Hex()) {
		return errors.New("wallet signer mismatch")
	}
	return nil
}
