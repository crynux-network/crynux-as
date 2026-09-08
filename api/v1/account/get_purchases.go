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

type GetPurchasesInput struct {
	Offset int `query:"offset" description:"The offset of the purchase records"`
	Limit  int `query:"limit" description:"The maximum number of the purchase records to return"`
}

type GetPurchasesData struct {
	Purchases []models.Deposit `json:"purchases" description:"The purchase records of the account"`
	Total     int64            `json:"total" description:"The total number of the purchase records"`
}

type GetPurchasesResponse struct {
	response.Response
	Data *GetPurchasesData `json:"data"`
}

func GetPurchases(c *gin.Context, in *GetPurchasesInput) (*GetPurchasesResponse, error) {
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

	user, err := models.FindUserByAddress(ctx, db, address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user for purchases: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var total int64
	if err := db.WithContext(dbCtx).Model(&models.Deposit{}).Where("user_id = ?", user.ID).Count(&total).Error; err != nil {
		log.Errorf("Error counting purchases for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	purchases := make([]models.Deposit, 0)
	if err := db.WithContext(dbCtx).
		Where("user_id = ?", user.ID).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&purchases).Error; err != nil {
		log.Errorf("Error listing purchases for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &GetPurchasesResponse{
		Data: &GetPurchasesData{
			Purchases: purchases,
			Total:     total,
		},
	}, nil
}
