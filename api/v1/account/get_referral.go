package account

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type PurchaseBonusTierData struct {
	MinPurchases uint64 `json:"min_purchases" description:"Invitee purchase count that must already be reached before this tier applies"`
	ExtraRatio   string `json:"extra_ratio" description:"Additional deposit reward ratio granted after min_purchases is reached"`
}

type GetReferralData struct {
	InviteCode           string                  `json:"invite_code" description:"The active invite code of the account"`
	InviteeCount         int64                   `json:"invitee_count" description:"Number of users bound through this account's invite codes"`
	InviteePurchaseCount int64                   `json:"invitee_purchase_count" description:"Number of deposits made by bound invitees"`
	DepositRewardRatio   string                  `json:"deposit_reward_ratio" description:"Configured base deposit reward ratio"`
	InviteeBonus         uint64                  `json:"invitee_bonus" description:"Configured invitee signup bonus Credits"`
	PurchaseBonusTiers   []PurchaseBonusTierData `json:"purchase_bonus_tiers" description:"Configured purchase-count bonus tiers"`
}

type GetReferralResponse struct {
	response.Response
	Data *GetReferralData `json:"data"`
}

func GetReferral(c *gin.Context) (*GetReferralResponse, error) {
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
		log.Errorf("Error loading user for referral: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	codeRow, err := service.EnsureActiveReferralCode(ctx, db, user.ID)
	if err != nil {
		log.Errorf("Error ensuring referral code for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var inviteeCount int64
	if err := db.WithContext(dbCtx).Model(&models.ReferralBinding{}).
		Where("referrer_user_id = ?", user.ID).
		Count(&inviteeCount).Error; err != nil {
		log.Errorf("Error counting invitees for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	purchaseCount, err := service.CountInviteePurchases(db.WithContext(dbCtx), user.ID, 0)
	if err != nil {
		log.Errorf("Error counting invitee purchases for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	cfg := config.GetConfig().Credits.Referral
	tiers := make([]PurchaseBonusTierData, 0, len(cfg.PurchaseBonusTiers))
	for _, tier := range cfg.PurchaseBonusTiers {
		tiers = append(tiers, PurchaseBonusTierData{
			MinPurchases: tier.MinPurchases,
			ExtraRatio:   tier.ExtraRatio,
		})
	}

	return &GetReferralResponse{
		Data: &GetReferralData{
			InviteCode:           codeRow.Code,
			InviteeCount:         inviteeCount,
			InviteePurchaseCount: purchaseCount,
			DepositRewardRatio:   cfg.DepositRewardRatio,
			InviteeBonus:         cfg.InviteeBonus,
			PurchaseBonusTiers:   tiers,
		},
	}, nil
}
