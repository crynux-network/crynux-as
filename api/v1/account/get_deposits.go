package account

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

func GetDeposits(c *gin.Context, in *GetDepositsInput) (*GetDepositsResponse, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}

	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	user, err := findUserByAddress(ctx, db, address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user for deposits: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var total int64
	if err := db.WithContext(dbCtx).Model(&models.Deposit{}).Where("user_id = ?", user.ID).Count(&total).Error; err != nil {
		log.Errorf("Error counting deposits for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	deposits := make([]models.Deposit, 0)
	if err := db.WithContext(dbCtx).
		Where("user_id = ?", user.ID).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&deposits).Error; err != nil {
		log.Errorf("Error listing deposits for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &GetDepositsResponse{
		Data: &GetDepositsData{
			Deposits: deposits,
			Total:    total,
		},
	}, nil
}
