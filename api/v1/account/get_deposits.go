package account

import (
	"crynux_as/api/v1/response"
	"crynux_as/models"
	"errors"

	"github.com/gin-gonic/gin"
)

type GetDepositsInput struct {
	Offset int `query:"offset" description:"The offset of the deposit records"`
	Limit  int `query:"limit" description:"The maximum number of the deposit records to return"`
}

type GetDepositsData struct {
	Deposits []models.Deposit `json:"deposits" description:"The deposit records of the account"`
	Total    int64            `json:"total" description:"The total number of the deposit records"`
}

type GetDepositsResponse struct {
	response.Response
	Data *GetDepositsData `json:"data"`
}

func GetDeposits(_ *gin.Context, _ *GetDepositsInput) (*GetDepositsResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
