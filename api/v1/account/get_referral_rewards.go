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

type ReferralRewardItem struct {
	ID                 uint           `json:"id" description:"Referral reward ID"`
	CreatedAt          time.Time      `json:"created_at" description:"When the reward was granted"`
	Kind               string         `json:"kind" description:"deposit_reward or milestone_bonus"`
	Amount             models.BigInt  `json:"amount" description:"Total Credits granted to the referrer"`
	InviteeAddress     *string        `json:"invitee_address" description:"Invitee wallet address for deposit rewards"`
	DepositCredits     *models.BigInt `json:"deposit_credits" description:"Invitee deposit Credits for deposit rewards"`
	PriorPurchaseCount uint64         `json:"prior_purchase_count" description:"Invitee purchase count before this deposit"`
	TierMinPurchases   *uint64        `json:"tier_min_purchases" description:"Highest purchase-bonus tier min_purchases applied to this reward"`
	ExtraRatio         string         `json:"extra_ratio" description:"Sum of applied purchase-bonus tier extra ratios"`
	ExtraAmount        models.BigInt  `json:"extra_amount" description:"Credits from applied purchase-bonus tiers"`
}

type GetReferralRewardsData struct {
	Rewards []ReferralRewardItem `json:"rewards" description:"Referral rewards earned by the account"`
	Total   int64                `json:"total" description:"Total number of referral rewards"`
}

type GetReferralRewardsInput struct {
	Offset int `query:"offset" description:"The offset of the reward records"`
	Limit  int `query:"limit" description:"The maximum number of reward records to return"`
}

type GetReferralRewardsResponse struct {
	response.Response
	Data *GetReferralRewardsData `json:"data"`
}

func referralRewardKindString(kind models.ReferralRewardKind) string {
	switch kind {
	case models.ReferralRewardKindDepositReward:
		return "deposit_reward"
	case models.ReferralRewardKindMilestoneBonus:
		return "milestone_bonus"
	default:
		return "unknown"
	}
}

func GetReferralRewards(c *gin.Context, in *GetReferralRewardsInput) (*GetReferralRewardsResponse, error) {
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
		log.Errorf("Error loading user for referral rewards: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var total int64
	if err := db.WithContext(dbCtx).Model(&models.ReferralReward{}).
		Where("referrer_user_id = ?", user.ID).
		Count(&total).Error; err != nil {
		log.Errorf("Error counting referral rewards for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	rewards := make([]models.ReferralReward, 0)
	if err := db.WithContext(dbCtx).
		Where("referrer_user_id = ?", user.ID).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&rewards).Error; err != nil {
		log.Errorf("Error listing referral rewards for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	inviteeIDs := make([]uint, 0)
	inviteeIDSet := make(map[uint]struct{})
	for _, reward := range rewards {
		if reward.InviteeUserID == nil {
			continue
		}
		if _, ok := inviteeIDSet[*reward.InviteeUserID]; ok {
			continue
		}
		inviteeIDSet[*reward.InviteeUserID] = struct{}{}
		inviteeIDs = append(inviteeIDs, *reward.InviteeUserID)
	}

	addressByID := make(map[uint]string, len(inviteeIDs))
	if len(inviteeIDs) > 0 {
		var users []models.User
		if err := db.WithContext(dbCtx).
			Where("id IN ?", inviteeIDs).
			Find(&users).Error; err != nil {
			log.Errorf("Error loading invitee addresses for user %d: %v", user.ID, err)
			return nil, response.NewExceptionResponse(err)
		}
		for _, u := range users {
			addressByID[u.ID] = u.Address
		}
	}

	items := make([]ReferralRewardItem, 0, len(rewards))
	for _, reward := range rewards {
		item := ReferralRewardItem{
			ID:                 reward.ID,
			CreatedAt:          reward.CreatedAt,
			Kind:               referralRewardKindString(reward.Kind),
			Amount:             reward.Amount,
			PriorPurchaseCount: reward.PriorPurchaseCount,
			TierMinPurchases:   reward.TierMinPurchases,
			ExtraRatio:         reward.ExtraRatio,
			ExtraAmount:        reward.ExtraAmount,
		}
		if reward.Kind == models.ReferralRewardKindDepositReward {
			credits := reward.DepositCredits
			item.DepositCredits = &credits
			if reward.InviteeUserID != nil {
				if addr, ok := addressByID[*reward.InviteeUserID]; ok {
					item.InviteeAddress = &addr
				}
			}
		}
		items = append(items, item)
	}

	return &GetReferralRewardsResponse{
		Data: &GetReferralRewardsData{
			Rewards: items,
			Total:   total,
		},
	}, nil
}
