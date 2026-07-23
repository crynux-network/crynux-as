package account

import (
	"crynux_as/api/v1/response"
	"crynux_as/models"
	"errors"

	"github.com/gin-gonic/gin"
)

type GetBalanceData struct {
	Address string        `json:"address" description:"The wallet address of the user"`
	Balance models.BigInt `json:"balance" description:"The Credits balance of the account"`
}

type GetBalanceResponse struct {
	response.Response
	Data *GetBalanceData `json:"data"`
}

func GetBalance(_ *gin.Context) (*GetBalanceResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
