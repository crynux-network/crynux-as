package auth

import (
	"crynux_as/api/v1/response"
	"errors"

	"github.com/gin-gonic/gin"
)

type LoginInput struct {
	Address   string `json:"address" validate:"required" description:"The wallet address of the user"`
	Timestamp int64  `json:"timestamp" validate:"required" description:"The unix timestamp when the message is signed"`
	Signature string `json:"signature" validate:"required" description:"The wallet signature of the login message"`
}

type LoginData struct {
	Token     string `json:"token" description:"The JWT token for subsequent API calls"`
	ExpiresAt int64  `json:"expires_at" description:"The unix timestamp when the token expires"`
}

type LoginResponse struct {
	response.Response
	Data *LoginData `json:"data"`
}

func Login(_ *gin.Context, _ *LoginInput) (*LoginResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
