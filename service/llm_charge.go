package service

import (
	"context"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInsufficientBalance = errors.New("insufficient credits balance")

type LLMPrices struct {
	PromptCreditsPerToken     uint64
	CompletionCreditsPerToken uint64
}

// CalcCredits computes Credits from token usage, project token ratio, and unit prices:
// credits = (prompt_tokens * ratioInt * promptPrice + completion_tokens * ratioInt * completionPrice) / 10
// CalcCredits computes credits = (P * R * V * Pp + C * R * V * Cp) / 100, where
// R is the stored token ratio (display * 10) and V is the stored VRAM tier ratio
// (display * 10). The result truncates toward zero.
func CalcCredits(promptTokens, completionTokens uint64, tokenRatio uint, vramRatio uint, prices LLMPrices) *big.Int {
	ratio := new(big.Int).SetUint64(uint64(tokenRatio))
	ratio.Mul(ratio, new(big.Int).SetUint64(uint64(vramRatio)))

	promptPart := new(big.Int).SetUint64(promptTokens)
	promptPart.Mul(promptPart, ratio)
	promptPart.Mul(promptPart, new(big.Int).SetUint64(prices.PromptCreditsPerToken))

	completionPart := new(big.Int).SetUint64(completionTokens)
	completionPart.Mul(completionPart, ratio)
	completionPart.Mul(completionPart, new(big.Int).SetUint64(prices.CompletionCreditsPerToken))

	total := new(big.Int).Add(promptPart, completionPart)
	return total.Div(total, big.NewInt(100))
}

// ResolveMaxCompletionTokens returns max_completion_tokens, else max_tokens, else defaultMax.
func ResolveMaxCompletionTokens(maxTokens, maxCompletionTokens *int, defaultMax uint64) uint64 {
	if maxCompletionTokens != nil && *maxCompletionTokens > 0 {
		return uint64(*maxCompletionTokens)
	}
	if maxTokens != nil && *maxTokens > 0 {
		return uint64(*maxTokens)
	}
	return defaultMax
}

// EstimatePromptTokensFromChatBody estimates prompt tokens from a chat completions JSON body
// as ceil(rune_count / 4), or 1 when the text is non-empty and the estimate would be 0.
func EstimatePromptTokensFromChatBody(body []byte) uint64 {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 1
	}
	var runes int
	for _, msg := range req.Messages {
		runes += contentRuneCount(msg.Content)
	}
	return runeCountToTokens(runes)
}

// EstimatePromptTokensFromCompletionsBody estimates prompt tokens from a completions JSON body.
func EstimatePromptTokensFromCompletionsBody(body []byte) uint64 {
	var req struct {
		Prompt json.RawMessage `json:"prompt"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 1
	}
	return runeCountToTokens(promptRuneCount(req.Prompt))
}

func runeCountToTokens(runes int) uint64 {
	if runes <= 0 {
		return 0
	}
	tokens := uint64(math.Ceil(float64(runes) / 4.0))
	if tokens == 0 {
		return 1
	}
	return tokens
}

func contentRuneCount(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return utf8.RuneCountInString(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		n := 0
		for _, p := range parts {
			n += utf8.RuneCountInString(p.Text)
		}
		return n
	}
	return utf8.RuneCount(raw)
}

func promptRuneCount(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return utf8.RuneCountInString(text)
	}
	var texts []string
	if err := json.Unmarshal(raw, &texts); err == nil {
		n := 0
		for _, t := range texts {
			n += utf8.RuneCountInString(t)
		}
		return n
	}
	return utf8.RuneCount(raw)
}

type RecordLLMCallInput struct {
	UserID           uint
	ProjectID        uint
	Model            string
	PromptTokens     uint64
	CompletionTokens uint64
	TotalTokens      uint64
	Status           models.LLMCallStatus
	Credits          *big.Int
	DurationMs       uint64
	BilledVram       uint64
	Charge           bool
}

// ProcessLLMCall writes an llm_call_records row and, for a successful chargeable call,
// applies the Credits ledger debit in the same transaction. When the balance is
// insufficient at settle time, the record is stored as success with Credits=0 and
// no ledger event is created.
func ProcessLLMCall(ctx context.Context, db *gorm.DB, in RecordLLMCallInput) error {
	if in.UserID == 0 {
		return errors.New("user id is required")
	}
	if in.ProjectID == 0 {
		return errors.New("project id is required")
	}
	credits := big.NewInt(0)
	if in.Credits != nil {
		credits = new(big.Int).Set(in.Credits)
	}
	if credits.Sign() < 0 {
		return errors.New("credits must be non-negative")
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		chargeCredits := new(big.Int).Set(credits)
		shouldCharge := in.Charge && in.Status == models.LLMCallStatusSuccess && chargeCredits.Sign() > 0

		if shouldCharge {
			var account models.CreditAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", in.UserID).
				First(&account).Error; err != nil {
				return err
			}
			if account.Balance.Cmp(chargeCredits) < 0 {
				log.Errorf(
					"insufficient balance at LLM settle: user_id=%d project_id=%d required=%s balance=%s",
					in.UserID, in.ProjectID, chargeCredits.String(), account.Balance.String(),
				)
				chargeCredits = big.NewInt(0)
				shouldCharge = false
			} else {
				newBalance := new(big.Int).Sub(&account.Balance.Int, chargeCredits)
				account.Balance = models.BigInt{Int: *newBalance}
				if err := tx.Save(&account).Error; err != nil {
					return err
				}
			}
		}

		record := models.LLMCallRecord{
			ProjectID:        in.ProjectID,
			Model:            in.Model,
			PromptTokens:     in.PromptTokens,
			CompletionTokens: in.CompletionTokens,
			TotalTokens:      in.TotalTokens,
			Status:           in.Status,
			Credits:          models.BigInt{Int: *chargeCredits},
			DurationMs:       in.DurationMs,
			BilledVram:       in.BilledVram,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}

		if !shouldCharge {
			return nil
		}

		event := models.CreditEvent{
			UserID: in.UserID,
			Amount: models.BigInt{Int: *new(big.Int).Set(chargeCredits)},
			Type:   models.CreditEventTypeLLMCharge,
			RefID:  record.ID,
			Status: models.CreditEventStatusProcessed,
		}
		if err := tx.Create(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("credit event already exists for llm call %d", record.ID)
			}
			return err
		}
		return nil
	})
}

// EnsureSufficientBalance returns ErrInsufficientBalance when balance is below required.
func EnsureSufficientBalance(balance, required *big.Int) error {
	if required == nil || required.Sign() <= 0 {
		return nil
	}
	if balance == nil || balance.Cmp(required) < 0 {
		return ErrInsufficientBalance
	}
	return nil
}
