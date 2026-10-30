package earn

import (
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type successResponse struct {
	response.Response
	Data any `json:"data"`
}

func loadUser(c *gin.Context) (*models.User, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}
	user, err := models.FindUserByAddress(c.Request.Context(), config.GetDB(), address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		return nil, response.NewExceptionResponse(err)
	}
	return user, nil
}

func keyItem(key models.SharedAPIKey) gin.H {
	var cooldownUntil any
	if key.CooldownUntil != nil && key.CooldownUntil.After(time.Now()) {
		cooldownUntil = key.CooldownUntil.UTC().Format(time.RFC3339)
	}
	return gin.H{
		"id":                 key.PublicID,
		"service_provider":   key.ServiceProvider,
		"masked_suffix":      key.MaskedSuffix,
		"owner_enabled":      key.OwnerEnabled,
		"used_input_tokens":  key.UsedInputTokens,
		"used_output_tokens": key.UsedOutputTokens,
		"max_input_tokens":   key.MaxInputTokens,
		"max_output_tokens":  key.MaxOutputTokens,
		"cooldown_until":     cooldownUntil,
	}
}

func GetSummary(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	account, err := service.EnsureEarningAccount(c.Request.Context(), config.GetDB(), user.ID)
	if err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	enabled, err := service.CountEnabledSharedAPIKeys(c.Request.Context(), config.GetDB(), user.ID)
	if err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	active, err := service.CountActiveProviderCallsForUser(c.Request.Context(), config.GetDB(), user.ID)
	if err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	return &successResponse{Data: gin.H{
		"address":           user.Address,
		"asset":             "CNX",
		"decimals":          18,
		"available":         account.Available.String(),
		"withdrawal_locked": account.WithdrawalLocked.String(),
		"enabled_key_count": enabled,
		"active_call_count": active,
	}}, nil
}

func ListProviders(c *gin.Context) (*successResponse, error) {
	if _, err := loadUser(c); err != nil {
		return nil, err
	}
	cfg := config.GetConfig()
	items := make([]gin.H, 0, len(cfg.Provider.Services))
	for id, svc := range cfg.Provider.Services {
		items = append(items, gin.H{
			"id":                     id,
			"display_name":           svc.DisplayName,
			"configured_model_count": len(svc.Models),
		})
	}
	return &successResponse{Data: items}, nil
}

func ListKeys(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	keys, err := service.ListSharedAPIKeys(c.Request.Context(), config.GetDB(), user.ID)
	if err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	items := make([]gin.H, 0, len(keys))
	for _, key := range keys {
		items = append(items, keyItem(key))
	}
	return &successResponse{Data: items}, nil
}

type createKeyBody struct {
	ServiceProvider string `json:"service_provider"`
	APIKey          string `json:"api_key"`
}

func CreateKey(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	var body createKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	secret := []byte(body.APIKey)
	defer service.ClearBytes(secret)
	key, err := service.CreateSharedAPIKey(c.Request.Context(), config.GetDB(), service.CreateSharedAPIKeyInput{
		UserID:          user.ID,
		ServiceProvider: models.ServiceProvider(body.ServiceProvider),
		APIKey:          secret,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSharedAPIKeyDuplicate):
			return nil, &conflictError{message: "duplicate shared api key"}
		case errors.Is(err, service.ErrSharedAPIKeyProvider), errors.Is(err, service.ErrSharedAPIKeyInvalidSecret):
			return nil, response.NewValidationErrorResponse("api_key", err.Error())
		case strings.Contains(err.Error(), "temporarily unavailable"):
			return nil, &serviceUnavailableError{message: err.Error()}
		default:
			log.Errorf("create shared api key failed: %v", err)
			return nil, response.NewExceptionResponse(err)
		}
	}
	return &successResponse{Data: keyItem(*key)}, nil
}

func EnableKey(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	key, err := service.EnableSharedAPIKey(c.Request.Context(), config.GetDB(), user.ID, c.Param("key_id"))
	if err != nil {
		return mapKeyErr(err)
	}
	return &successResponse{Data: keyItem(*key)}, nil
}

func DisableKey(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	key, err := service.DisableSharedAPIKey(c.Request.Context(), config.GetDB(), user.ID, c.Param("key_id"))
	if err != nil {
		return mapKeyErr(err)
	}
	return &successResponse{Data: keyItem(*key)}, nil
}

func DeleteKey(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	if err := service.DeleteSharedAPIKey(c.Request.Context(), config.GetDB(), user.ID, c.Param("key_id")); err != nil {
		return mapKeyErr(err)
	}
	return &successResponse{Data: gin.H{}}, nil
}

func parseOptionalUint64Limit(raw map[string]any, field string) (*uint64, error) {
	v, ok := raw[field]
	if !ok {
		return nil, response.NewValidationErrorResponse(field, "is required")
	}
	if v == nil {
		return nil, nil
	}
	switch n := v.(type) {
	case float64:
		if n != float64(uint64(n)) || n <= 0 {
			return nil, response.NewValidationErrorResponse(field, "must be a positive integer or null")
		}
		u := uint64(n)
		return &u, nil
	default:
		return nil, response.NewValidationErrorResponse(field, "must be a positive integer or null")
	}
}

func SetKeyLimits(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	maxInput, err := parseOptionalUint64Limit(raw, "max_input_tokens")
	if err != nil {
		return nil, err
	}
	maxOutput, err := parseOptionalUint64Limit(raw, "max_output_tokens")
	if err != nil {
		return nil, err
	}
	key, err := service.UpdateSharedAPIKeyLimits(c.Request.Context(), config.GetDB(), user.ID, c.Param("key_id"), maxInput, maxOutput)
	if err != nil {
		return mapKeyErr(err)
	}
	return &successResponse{Data: keyItem(*key)}, nil
}

func ResetKeyUsage(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	key, err := service.ResetSharedAPIKeyUsage(c.Request.Context(), config.GetDB(), user.ID, c.Param("key_id"))
	if err != nil {
		return mapKeyErr(err)
	}
	return &successResponse{Data: keyItem(*key)}, nil
}

func mapKeyErr(err error) (*successResponse, error) {
	if errors.Is(err, service.ErrSharedAPIKeyNotFound) {
		return nil, response.NewNotFoundErrorResponse()
	}
	if errors.Is(err, service.ErrSharedAPIKeyDeleted) {
		return nil, response.NewValidationErrorResponse("key_id", err.Error())
	}
	if errors.Is(err, service.ErrSharedAPIKeyInvalidLimit) {
		return nil, response.NewValidationErrorResponse("max_input_tokens", err.Error())
	}
	return nil, response.NewExceptionResponse(err)
}

func ListWithdrawalNetworks(c *gin.Context) (*successResponse, error) {
	if _, err := loadUser(c); err != nil {
		return nil, err
	}
	cfg := config.GetConfig()
	items := make([]gin.H, 0, len(cfg.Provider.Wallet.Networks))
	for id, network := range cfg.Provider.Wallet.Networks {
		chainID := uint64(0)
		if bc, ok := cfg.Blockchains[id]; ok {
			chainID = bc.ChainID
		}
		items = append(items, gin.H{
			"id":                       id,
			"display_name":             network.DisplayName,
			"chain_id":                 chainID,
			"asset":                    "CNX",
			"decimals":                 18,
			"fixed_withdrawal_fee_cnx": network.FixedWithdrawalFeeCNX,
			"withdrawal_min_cnx":       network.WithdrawalMinCNX,
		})
	}
	return &successResponse{Data: items}, nil
}

type prepareBody struct {
	Amount  string `json:"amount"`
	Network string `json:"network"`
}

func PrepareWithdrawal(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	var body prepareBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	amount, ok := new(big.Int).SetString(body.Amount, 10)
	if !ok {
		return nil, response.NewValidationErrorResponse("amount", "invalid amount")
	}
	result, err := service.PrepareWithdrawal(c.Request.Context(), config.GetDB(), user, amount, body.Network)
	if err != nil {
		if errors.Is(err, service.ErrWithdrawalBelowMinimum) {
			return nil, response.NewValidationErrorResponse("amount", err.Error())
		}
		if errors.Is(err, service.ErrWithdrawalNetwork) {
			return nil, response.NewValidationErrorResponse("network", err.Error())
		}
		return nil, response.NewValidationErrorResponse("amount", err.Error())
	}
	return &successResponse{Data: result}, nil
}

type createWithdrawalBody struct {
	Amount         string `json:"amount"`
	Network        string `json:"network"`
	BenefitAddress string `json:"benefit_address"`
	Timestamp      int64  `json:"timestamp"`
	Signature      string `json:"signature"`
}

func CreateWithdrawal(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	var body createWithdrawalBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	amount, ok := new(big.Int).SetString(body.Amount, 10)
	if !ok {
		return nil, response.NewValidationErrorResponse("amount", "invalid amount")
	}
	withdrawal, err := service.CreateWithdrawal(c.Request.Context(), config.GetDB(), service.CreateWithdrawalInput{
		User:           user,
		Amount:         amount,
		Network:        body.Network,
		BenefitAddress: body.BenefitAddress,
		Timestamp:      body.Timestamp,
		Signature:      body.Signature,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInsufficientEarnings):
			return nil, &paymentRequiredError{message: err.Error()}
		case errors.Is(err, service.ErrWithdrawalBelowMinimum):
			return nil, response.NewValidationErrorResponse("amount", err.Error())
		case errors.Is(err, service.ErrWithdrawalDailyLimit):
			return nil, &rateLimitError{message: err.Error()}
		case errors.Is(err, service.ErrWithdrawalAuthReplay):
			return nil, &conflictError{message: err.Error()}
		default:
			return nil, response.NewValidationErrorResponse("withdrawal", err.Error())
		}
	}
	cfg := config.GetConfig()
	chainID := uint64(0)
	if bc, ok := cfg.Blockchains[withdrawal.Network]; ok {
		chainID = bc.ChainID
	}
	return &successResponse{Data: gin.H{
		"id":              withdrawal.ID,
		"amount":          withdrawal.Amount.String(),
		"withdrawal_fee":  withdrawal.WithdrawalFee.String(),
		"network":         withdrawal.Network,
		"chain_id":        chainID,
		"address":         user.Address,
		"benefit_address": withdrawal.BenefitAddress,
		"status":          "pending",
		"tx_hash":         nil,
		"created_at":      withdrawal.CreatedAt.UTC().Format(time.RFC3339),
	}}, nil
}

func ListWithdrawals(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	db := config.GetDB()
	var beforeID uint64
	if raw := c.Query("before_id"); raw != "" {
		beforeID, _ = parseUint(raw)
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if v, err := parseUint(raw); err == nil && v > 0 && v <= 100 {
			limit = int(v)
		}
	}
	q := db.WithContext(c.Request.Context()).Model(&models.EarningWithdrawal{}).Where("user_id = ?", user.ID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var ids []uint
	if err := q.Order("id DESC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	var rows []models.EarningWithdrawal
	if len(ids) > 0 {
		if err := db.Where("id IN ?", ids).Order("id DESC").Find(&rows).Error; err != nil {
			return nil, response.NewExceptionResponse(err)
		}
	}
	cfg := config.GetConfig()
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		status := "pending"
		switch row.Status {
		case models.EarningWithdrawalStatusSuccess:
			status = "success"
		case models.EarningWithdrawalStatusFailed:
			status = "failed"
		}
		chainID := uint64(0)
		if bc, ok := cfg.Blockchains[row.Network]; ok {
			chainID = bc.ChainID
		}
		var tx any
		if row.TxHash != nil {
			tx = *row.TxHash
		}
		items = append(items, gin.H{
			"id":              row.ID,
			"amount":          row.Amount.String(),
			"withdrawal_fee":  row.WithdrawalFee.String(),
			"network":         row.Network,
			"chain_id":        chainID,
			"address":         user.Address,
			"benefit_address": row.BenefitAddress,
			"status":          status,
			"tx_hash":         tx,
			"created_at":      row.CreatedAt.UTC().Format(time.RFC3339),
			"updated_at":      row.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	var next any
	if len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	return &successResponse{Data: gin.H{"items": items, "next_before_id": next}}, nil
}

func ListCalls(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	db := config.GetDB()
	var keyIDs []uint
	if err := db.WithContext(c.Request.Context()).Model(&models.SharedAPIKey{}).
		Where("user_id = ?", user.ID).
		Pluck("id", &keyIDs).Error; err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	if len(keyIDs) == 0 {
		return &successResponse{Data: gin.H{"items": []any{}, "next_before_id": nil}}, nil
	}
	var beforeID uint64
	if raw := c.Query("before_id"); raw != "" {
		beforeID, _ = parseUint(raw)
	}
	limit := 50
	q := db.WithContext(c.Request.Context()).Model(&models.TaskCallRecord{}).
		Where("shared_api_key_id IN ?", keyIDs)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var ids []uint
	if err := q.Order("id DESC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	var records []models.TaskCallRecord
	if len(ids) > 0 {
		if err := db.Where("id IN ?", ids).Order("id DESC").Find(&records).Error; err != nil {
			return nil, response.NewExceptionResponse(err)
		}
	}
	var keys []models.SharedAPIKey
	_ = db.Where("id IN ?", keyIDs).Find(&keys)
	keyByID := map[uint]models.SharedAPIKey{}
	for _, key := range keys {
		keyByID[key.ID] = key
	}
	items := make([]gin.H, 0, len(records))
	for _, record := range records {
		key := keyByID[*record.SharedAPIKeyID]
		earning := "0"
		var event models.EarningEvent
		if err := db.Where("event_type = ? AND source_type = ? AND source_id = ?",
			models.EarningEventTypeTaskIncome, models.EarningSourceTypeTaskCallRecord, record.ID).
			First(&event).Error; err == nil {
			earning = event.Amount.String()
		}
		status := "success"
		if record.Status == models.TaskCallStatusFailed {
			status = "failed"
		}
		items = append(items, gin.H{
			"id":                record.ID,
			"service_provider":  key.ServiceProvider,
			"model":             record.Model,
			"shared_api_key_id": key.PublicID,
			"key_masked_suffix": key.MaskedSuffix,
			"status":            status,
			"prompt_tokens":     record.PromptTokens,
			"completion_tokens": record.CompletionTokens,
			"total_tokens":      record.TotalTokens,
			"priority_gwei":     record.PriorityGwei.String(),
			"earning_wei":       earning,
			"accepted_at":       record.AcceptedAt.UTC().Format(time.RFC3339),
			"completed_at":      record.CompletedAt.UTC().Format(time.RFC3339),
			"duration_ms":       record.DurationMs,
		})
	}
	var next any
	if len(records) > 0 {
		next = records[len(records)-1].ID
	}
	return &successResponse{Data: gin.H{"items": items, "next_before_id": next}}, nil
}

func ListEvents(c *gin.Context) (*successResponse, error) {
	user, err := loadUser(c)
	if err != nil {
		return nil, err
	}
	account, err := service.EnsureEarningAccount(c.Request.Context(), config.GetDB(), user.ID)
	if err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	db := config.GetDB()
	var beforeID uint64
	if raw := c.Query("before_id"); raw != "" {
		beforeID, _ = parseUint(raw)
	}
	q := db.WithContext(c.Request.Context()).Model(&models.EarningEvent{}).
		Where("earning_account_id = ? AND event_type = ? AND status = ?",
			account.ID, models.EarningEventTypeTaskIncome, models.EarningEventStatusProcessed)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var ids []uint
	if err := q.Order("id DESC").Limit(50).Pluck("id", &ids).Error; err != nil {
		return nil, response.NewExceptionResponse(err)
	}
	var events []models.EarningEvent
	if len(ids) > 0 {
		if err := db.Where("id IN ?", ids).Order("id DESC").Find(&events).Error; err != nil {
			return nil, response.NewExceptionResponse(err)
		}
	}
	items := make([]gin.H, 0, len(events))
	for _, event := range events {
		var record models.TaskCallRecord
		_ = db.First(&record, event.SourceID)
		processed := ""
		if event.ProcessedAt != nil {
			processed = event.ProcessedAt.UTC().Format(time.RFC3339)
		}
		items = append(items, gin.H{
			"id":               event.ID,
			"type":             "task_income",
			"amount":           event.Amount.String(),
			"status":           "available",
			"service_provider": record.ExecutionSource,
			"model":            record.Model,
			"created_at":       event.CreatedAt.UTC().Format(time.RFC3339),
			"processed_at":     processed,
		})
	}
	var next any
	if len(events) > 0 {
		next = events[len(events)-1].ID
	}
	return &successResponse{Data: gin.H{"items": items, "next_before_id": next}}, nil
}

func parseUint(raw string) (uint64, error) {
	return strconv.ParseUint(raw, 10, 64)
}
