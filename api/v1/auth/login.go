package auth

import (
	"crynux_as/api/tools"
	"crynux_as/api/v1/response"
	"crynux_as/blockchain"
	"crynux_as/config"
	"crynux_as/service"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type LoginInput struct {
	Address    string `json:"address" validate:"required" description:"The wallet address of the user"`
	Timestamp  int64  `json:"timestamp" validate:"required" description:"The unix timestamp when the message is signed"`
	Signature  string `json:"signature" validate:"required" description:"The wallet signature of the login message"`
	InviteCode string `json:"invite_code" description:"Optional invite code applied only when the account is created"`
}

type LoginData struct {
	Token     string `json:"token" description:"The JWT token for subsequent API calls"`
	ExpiresAt int64  `json:"expires_at" description:"The unix timestamp when the token expires"`
}

type LoginResponse struct {
	response.Response
	Data *LoginData `json:"data"`
}

func Login(c *gin.Context, in *LoginInput) (*LoginResponse, error) {
	verifier := blockchain.NewSignatureVerifier()
	if err := verifier.ValidateAddress(in.Address); err != nil {
		return nil, response.NewValidationErrorResponse("address", "Invalid address")
	}

	message := tools.GenerateLoginMessage(in.Address, in.Timestamp)
	signerAddress, err := tools.ValidateAndRecover(message, in.Signature, in.Timestamp)
	if err != nil {
		log.Debugf("Error validating login signature: %v", err)
		return nil, response.NewValidationErrorResponse("signature", "Invalid signature")
	}

	if !strings.EqualFold(signerAddress, in.Address) {
		return nil, response.NewValidationErrorResponse("address", "Signature address mismatch")
	}

	normalized := common.HexToAddress(in.Address).Hex()
	if _, err := service.EnsureUserWithReferral(c.Request.Context(), config.GetDB(), normalized, in.InviteCode); err != nil {
		log.Errorf("Error ensuring user account for %s: %v", normalized, err)
		return nil, response.NewExceptionResponse(err)
	}

	token, exp, err := tools.GenerateToken(normalized)
	if err != nil {
		log.Errorf("Error generating JWT token: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	return &LoginResponse{
		Data: &LoginData{
			Token:     token,
			ExpiresAt: exp.Unix(),
		},
	}, nil
}
