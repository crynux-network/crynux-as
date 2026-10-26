package account

import (
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ResetReferralCodeData struct {
	InviteCode string `json:"invite_code" description:"The new active invite code"`
}

type ResetReferralCodeResponse struct {
	response.Response
	Data *ResetReferralCodeData `json:"data"`
}

func ResetReferralCode(c *gin.Context) (*ResetReferralCodeResponse, error) {
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
		log.Errorf("Error loading user for referral code reset: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	codeRow, err := service.ResetReferralCode(ctx, db, user.ID)
	if err != nil {
		log.Errorf("Error resetting referral code for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &ResetReferralCodeResponse{
		Data: &ResetReferralCodeData{
			InviteCode: codeRow.Code,
		},
	}, nil
}
