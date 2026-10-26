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

type PurchaseItem struct {
	ID          uint           `json:"id" description:"Credit event ID"`
	Type        string         `json:"type" description:"purchase, signup_bonus, or invitee_bonus"`
	Credits     models.BigInt  `json:"credits" description:"Credits credited by this record"`
	CreatedAt   time.Time      `json:"created_at" description:"When the Credits were credited"`
	Amount      *models.BigInt `json:"amount" description:"Raw token amount for purchases"`
	Token       *string        `json:"token" description:"Token symbol for purchases"`
	Network     *string        `json:"network" description:"Network name for purchases"`
	TxHash      *string        `json:"tx_hash" description:"Transaction hash for purchases"`
	FromAddress *string        `json:"from_address" description:"Sender address for purchases"`
	LogIndex    *uint          `json:"log_index" description:"Log index for purchases"`
}

type GetPurchasesData struct {
	Purchases []PurchaseItem `json:"purchases" description:"Purchase and bonus credit records of the account"`
	Total     int64          `json:"total" description:"The total number of purchase and bonus credit records"`
}

type GetPurchasesResponse struct {
	response.Response
	Data *GetPurchasesData `json:"data"`
}

func purchaseTypeString(t models.CreditEventType) string {
	switch t {
	case models.CreditEventTypeDeposit:
		return "purchase"
	case models.CreditEventTypeSignupBonus:
		return "signup_bonus"
	case models.CreditEventTypeInviteeBonus:
		return "invitee_bonus"
	default:
		return "unknown"
	}
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

	purchaseTypes := []models.CreditEventType{
		models.CreditEventTypeDeposit,
		models.CreditEventTypeSignupBonus,
		models.CreditEventTypeInviteeBonus,
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var total int64
	if err := db.WithContext(dbCtx).Model(&models.CreditEvent{}).
		Where("user_id = ? AND status = ? AND type IN ?", user.ID, models.CreditEventStatusProcessed, purchaseTypes).
		Count(&total).Error; err != nil {
		log.Errorf("Error counting purchases for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	events := make([]models.CreditEvent, 0)
	if err := db.WithContext(dbCtx).
		Where("user_id = ? AND status = ? AND type IN ?", user.ID, models.CreditEventStatusProcessed, purchaseTypes).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		log.Errorf("Error listing purchases for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	depositIDs := make([]uint, 0)
	for _, event := range events {
		if event.Type == models.CreditEventTypeDeposit {
			depositIDs = append(depositIDs, event.RefID)
		}
	}

	depositByID := make(map[uint]models.Deposit, len(depositIDs))
	if len(depositIDs) > 0 {
		var deposits []models.Deposit
		if err := db.WithContext(dbCtx).
			Where("id IN ?", depositIDs).
			Find(&deposits).Error; err != nil {
			log.Errorf("Error loading deposits for user %d purchases: %v", user.ID, err)
			return nil, response.NewExceptionResponse(err)
		}
		for _, deposit := range deposits {
			depositByID[deposit.ID] = deposit
		}
	}

	purchases := make([]PurchaseItem, 0, len(events))
	for _, event := range events {
		item := PurchaseItem{
			ID:        event.ID,
			Type:      purchaseTypeString(event.Type),
			Credits:   event.Amount,
			CreatedAt: event.CreatedAt,
		}
		if event.Type == models.CreditEventTypeDeposit {
			if deposit, ok := depositByID[event.RefID]; ok {
				amount := deposit.Amount
				token := deposit.Token
				network := deposit.Network
				txHash := deposit.TxHash
				fromAddress := deposit.FromAddress
				logIndex := deposit.LogIndex
				item.Amount = &amount
				item.Token = &token
				item.Network = &network
				item.TxHash = &txHash
				item.FromAddress = &fromAddress
				item.LogIndex = &logIndex
			}
		}
		purchases = append(purchases, item)
	}

	return &GetPurchasesResponse{
		Data: &GetPurchasesData{
			Purchases: purchases,
			Total:     total,
		},
	}, nil
}
