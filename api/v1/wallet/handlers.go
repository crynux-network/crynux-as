package wallet

import (
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type walletSuccessResponse struct {
	response.Response
	Data any `json:"data"`
}

type eventLogItem struct {
	ID        uint   `json:"id"`
	CreatedAt uint64 `json:"created_at"`
	Address   string `json:"address"`
	Amount    string `json:"amount"`
	Type      int8   `json:"type"`
	Payload   string `json:"payload"`
}

type withdrawalListItem struct {
	ID                  uint   `json:"id"`
	CreatedAt           uint64 `json:"created_at"`
	Address             string `json:"address"`
	BenefitAddress      string `json:"benefit_address"`
	Amount              string `json:"amount"`
	WithdrawalFee       string `json:"withdrawal_fee"`
	Network             string `json:"network"`
	Status              int8   `json:"status"`
	RelayAccountEventID uint   `json:"relay_account_event_id"`
	Timestamp           int64  `json:"timestamp"`
	Signature           string `json:"signature"`
}

func GetEventLogs(c *gin.Context) (*walletSuccessResponse, error) {
	startID, _ := strconv.ParseUint(c.Query("start_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	timestamp, _ := strconv.ParseInt(c.Query("timestamp"), 10, 64)
	signature := c.Query("signature")
	business := map[string]any{"limit": limit, "start_id": uint(startID)}
	if err := VerifyWalletSignature(business, timestamp, signature); err != nil {
		return nil, response.NewValidationErrorResponse("signature", err.Error())
	}
	events, err := service.ListProcessedEarningEventsForWallet(c.Request.Context(), config.GetDB(), uint(startID), limit)
	if err != nil {
		log.Errorf("wallet event logs failed: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	data := make([]eventLogItem, 0, len(events))
	for _, event := range events {
		data = append(data, eventLogItem{
			ID:        event.ID,
			CreatedAt: uint64(event.CreatedAt.Unix()),
			Address:   event.Address,
			Amount:    event.Amount.String(),
			Type:      int8(event.EventType),
			Payload:   event.Payload,
		})
	}
	return &walletSuccessResponse{Data: data}, nil
}

func GetWithdrawList(c *gin.Context) (*walletSuccessResponse, error) {
	startID, _ := strconv.ParseUint(c.Query("start_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	timestamp, _ := strconv.ParseInt(c.Query("timestamp"), 10, 64)
	signature := c.Query("signature")
	business := map[string]any{"limit": limit, "start_id": uint(startID)}
	if err := VerifyWalletSignature(business, timestamp, signature); err != nil {
		return nil, response.NewValidationErrorResponse("signature", err.Error())
	}
	rows, addresses, err := service.ListPendingWithdrawalsForWallet(c.Request.Context(), config.GetDB(), uint(startID), limit)
	if err != nil {
		log.Errorf("wallet withdraw list failed: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	data := make([]withdrawalListItem, 0, len(rows))
	for _, row := range rows {
		data = append(data, withdrawalListItem{
			ID:                  row.ID,
			CreatedAt:           uint64(row.CreatedAt.Unix()),
			Address:             addresses[row.UserID],
			BenefitAddress:      row.BenefitAddress,
			Amount:              row.Amount.String(),
			WithdrawalFee:       row.WithdrawalFee.String(),
			Network:             row.Network,
			Status:              int8(row.Status),
			RelayAccountEventID: derefUint(row.EarningEventID),
			Timestamp:           row.AuthorizationTimestamp,
			Signature:           row.AuthorizationSignature,
		})
	}
	return &walletSuccessResponse{Data: data}, nil
}

type fulfillBody struct {
	ID        uint   `json:"id"`
	TxHash    string `json:"tx_hash"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

func FulfillWithdrawal(c *gin.Context) (*walletSuccessResponse, error) {
	var body fulfillBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	pathID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if uint(pathID) != body.ID {
		return nil, response.NewValidationErrorResponse("id", "path and body id mismatch")
	}
	business := map[string]any{"id": body.ID, "tx_hash": body.TxHash}
	if err := VerifyWalletSignature(business, body.Timestamp, body.Signature); err != nil {
		return nil, response.NewValidationErrorResponse("signature", err.Error())
	}
	if err := service.FulfillWithdrawal(c.Request.Context(), config.GetDB(), body.ID, body.TxHash); err != nil {
		if errors.Is(err, service.ErrWithdrawalNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		if errors.Is(err, service.ErrWithdrawalConflict) {
			return nil, response.NewValidationErrorResponse("status", err.Error())
		}
		log.Errorf("wallet fulfill failed: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	return &walletSuccessResponse{Data: gin.H{}}, nil
}

type rejectBody struct {
	ID        uint   `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

func RejectWithdrawal(c *gin.Context) (*walletSuccessResponse, error) {
	var body rejectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, response.NewValidationErrorResponse("body", err.Error())
	}
	pathID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if uint(pathID) != body.ID {
		return nil, response.NewValidationErrorResponse("id", "path and body id mismatch")
	}
	business := map[string]any{"id": body.ID}
	if err := VerifyWalletSignature(business, body.Timestamp, body.Signature); err != nil {
		return nil, response.NewValidationErrorResponse("signature", err.Error())
	}
	if err := service.RejectWithdrawal(c.Request.Context(), config.GetDB(), body.ID); err != nil {
		if errors.Is(err, service.ErrWithdrawalNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		if errors.Is(err, service.ErrWithdrawalConflict) {
			return nil, response.NewValidationErrorResponse("status", err.Error())
		}
		log.Errorf("wallet reject failed: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	_ = models.EarningWithdrawalStatusPending
	return &walletSuccessResponse{Data: gin.H{}}, nil
}

func derefUint(v *uint) uint {
	if v == nil {
		return 0
	}
	return *v
}
