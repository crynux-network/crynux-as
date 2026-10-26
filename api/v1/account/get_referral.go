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

type GetReferralData struct {
	InviteCode           string `json:"invite_code" description:"The active invite code of the account"`
	InviteeCount         int64  `json:"invitee_count" description:"Number of users bound through this account's invite codes"`
	MilestoneReached     bool   `json:"milestone_reached" description:"Whether the milestone bonus has been granted"`
	DepositRewardRatio   string `json:"deposit_reward_ratio" description:"Configured deposit reward ratio"`
	InviteeBonus         uint64 `json:"invitee_bonus" description:"Configured invitee signup bonus Credits"`
	MilestoneInviteCount uint64 `json:"milestone_invite_count" description:"Configured invite count required for the milestone bonus"`
	MilestoneBonus       uint64 `json:"milestone_bonus" description:"Configured milestone bonus Credits"`
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

	var milestone models.ReferralReward
	milestoneErr := db.WithContext(dbCtx).
		Where("referrer_user_id = ? AND kind = ?", user.ID, models.ReferralRewardKindMilestoneBonus).
		First(&milestone).Error
	if milestoneErr != nil && !errors.Is(milestoneErr, gorm.ErrRecordNotFound) {
		log.Errorf("Error loading milestone reward for user %d: %v", user.ID, milestoneErr)
		return nil, response.NewExceptionResponse(milestoneErr)
	}

	cfg := config.GetConfig().Credits.Referral
	return &GetReferralResponse{
		Data: &GetReferralData{
			InviteCode:           codeRow.Code,
			InviteeCount:         inviteeCount,
			MilestoneReached:     milestoneErr == nil,
			DepositRewardRatio:   cfg.DepositRewardRatio,
			InviteeBonus:         cfg.InviteeBonus,
			MilestoneInviteCount: cfg.MilestoneInviteCount,
			MilestoneBonus:       cfg.MilestoneBonus,
		},
	}, nil
}
