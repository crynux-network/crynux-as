package account

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"math/big"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type GetBalanceData struct {
	Address   string        `json:"address" description:"The wallet address of the user"`
	Balance   models.BigInt `json:"balance" description:"The Credits balance of the account"`
	Locked    models.BigInt `json:"locked" description:"Credits reserved by in-flight tasks"`
	Available models.BigInt `json:"available" description:"Credits available for new tasks (balance - locked)"`
}

type GetBalanceResponse struct {
	response.Response
	Data *GetBalanceData `json:"data"`
}

func GetBalance(c *gin.Context) (*GetBalanceResponse, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	user, err := models.FindUserByAddress(ctx, db, address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user for balance: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var account models.CreditAccount
	if err := db.WithContext(dbCtx).Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			zero := models.BigInt{Int: *big.NewInt(0)}
			return &GetBalanceResponse{
				Data: &GetBalanceData{
					Address:   address,
					Balance:   zero,
					Locked:    zero,
					Available: zero,
				},
			}, nil
		}
		log.Errorf("Error loading credit account for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	available := service.AvailableCredits(&account.Balance.Int, &account.Locked.Int)
	return &GetBalanceResponse{
		Data: &GetBalanceData{
			Address:   address,
			Balance:   account.Balance,
			Locked:    account.Locked,
			Available: models.BigInt{Int: *available},
		},
	}, nil
}
