package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/utils"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrSharedAPIKeyNotFound      = errors.New("shared api key not found")
	ErrSharedAPIKeyDuplicate     = errors.New("shared api key already exists")
	ErrSharedAPIKeyDeleted       = errors.New("shared api key is deleted")
	ErrSharedAPIKeyInvalidSecret = errors.New("shared api key secret is invalid")
	ErrSharedAPIKeyProvider      = errors.New("service provider is not configured")
	ErrSharedAPIKeyInvalidLimit  = errors.New("shared api key token limit is invalid")
)

type SharedAPIKeyCredentialValidator interface {
	ValidateCredential(ctx context.Context, provider models.ServiceProvider, secret []byte) error
}

var sharedAPIKeyValidator SharedAPIKeyCredentialValidator

func SetSharedAPIKeyCredentialValidator(v SharedAPIKeyCredentialValidator) {
	sharedAPIKeyValidator = v
}

type CreateSharedAPIKeyInput struct {
	UserID          uint
	ServiceProvider models.ServiceProvider
	APIKey          []byte
}

func MaskAPIKeySuffix(secret []byte) string {
	if len(secret) == 0 {
		return ""
	}
	n := 4
	if len(secret) < n {
		n = len(secret)
	}
	return string(secret[len(secret)-n:])
}

func CreateSharedAPIKey(ctx context.Context, db *gorm.DB, in CreateSharedAPIKeyInput) (*models.SharedAPIKey, error) {
	cfg := config.GetConfig()
	if _, ok := cfg.Provider.Services[string(in.ServiceProvider)]; !ok {
		return nil, ErrSharedAPIKeyProvider
	}
	if len(in.APIKey) == 0 || uint64(len(in.APIKey)) > cfg.Provider.MaxSecretBytes {
		return nil, ErrSharedAPIKeyInvalidSecret
	}
	enc := GetProviderKeyEncryption()
	if enc == nil {
		return nil, errors.New("provider key encryption is not initialized")
	}
	if sharedAPIKeyValidator != nil {
		if err := sharedAPIKeyValidator.ValidateCredential(ctx, in.ServiceProvider, in.APIKey); err != nil {
			return nil, err
		}
	}

	fingerprint := FingerprintAPIKey(in.APIKey)
	publicID, err := utils.GenerateRandomToken(16)
	if err != nil {
		return nil, fmt.Errorf("generate shared api key public id: %w", err)
	}
	publicID = "key_" + publicID
	aad := SharedAPIKeyAAD(publicID, in.UserID, string(in.ServiceProvider))
	ciphertext, nonce, err := enc.Encrypt(in.APIKey, aad)
	if err != nil {
		return nil, err
	}
	masked := MaskAPIKeySuffix(in.APIKey)
	maxInputTokens := cfg.Provider.DefaultMaxInputTokens
	maxOutputTokens := cfg.Provider.DefaultMaxOutputTokens

	row := models.SharedAPIKey{
		PublicID:        publicID,
		UserID:          in.UserID,
		ServiceProvider: in.ServiceProvider,
		MaskedSuffix:    masked,
		OwnerEnabled:    true,
		MaxInputTokens:  &maxInputTokens,
		MaxOutputTokens: &maxOutputTokens,
		Fingerprint:     &fingerprint,
		KeyCiphertext:   ciphertext,
		KeyNonce:        nonce,
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.WithContext(dbCtx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrSharedAPIKeyDuplicate
		}
		return nil, err
	}
	return &row, nil
}

func ListSharedAPIKeys(ctx context.Context, db *gorm.DB, userID uint) ([]models.SharedAPIKey, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var rows []models.SharedAPIKey
	err := db.WithContext(dbCtx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("id DESC").
		Find(&rows).Error
	return rows, err
}

func GetSharedAPIKeyByPublicID(ctx context.Context, db *gorm.DB, userID uint, publicID string) (*models.SharedAPIKey, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var row models.SharedAPIKey
	err := db.WithContext(dbCtx).
		Where("user_id = ? AND public_id = ?", userID, publicID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSharedAPIKeyNotFound
		}
		return nil, err
	}
	return &row, nil
}

func EnableSharedAPIKey(ctx context.Context, db *gorm.DB, userID uint, publicID string) (*models.SharedAPIKey, error) {
	return setSharedAPIKeyEnabled(ctx, db, userID, publicID, true)
}

func DisableSharedAPIKey(ctx context.Context, db *gorm.DB, userID uint, publicID string) (*models.SharedAPIKey, error) {
	return setSharedAPIKeyEnabled(ctx, db, userID, publicID, false)
}

func setSharedAPIKeyEnabled(ctx context.Context, db *gorm.DB, userID uint, publicID string, enabled bool) (*models.SharedAPIKey, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var row models.SharedAPIKey
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND public_id = ?", userID, publicID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharedAPIKeyNotFound
			}
			return err
		}
		if row.DeletedAt != nil {
			return ErrSharedAPIKeyDeleted
		}
		row.OwnerEnabled = enabled
		return tx.Save(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func DeleteSharedAPIKey(ctx context.Context, db *gorm.DB, userID uint, publicID string) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var row models.SharedAPIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND public_id = ?", userID, publicID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharedAPIKeyNotFound
			}
			return err
		}
		if row.DeletedAt != nil {
			return nil
		}
		now := time.Now()
		return tx.Model(&row).Updates(map[string]interface{}{
			"deleted_at":     now,
			"owner_enabled":  false,
			"fingerprint":    nil,
			"key_ciphertext": nil,
			"key_nonce":      nil,
		}).Error
	})
}

// ListEligibleSharedAPIKeys returns database-eligible keys for a Service Provider.
func ListEligibleSharedAPIKeys(ctx context.Context, db *gorm.DB, provider models.ServiceProvider) ([]models.SharedAPIKey, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	var rows []models.SharedAPIKey
	err := db.WithContext(dbCtx).
		Where("service_provider = ? AND owner_enabled = ? AND deleted_at IS NULL AND (cooldown_until IS NULL OR cooldown_until <= ?)",
			provider, true, now).
		Where("(max_input_tokens IS NULL OR used_input_tokens < max_input_tokens)").
		Where("(max_output_tokens IS NULL OR used_output_tokens < max_output_tokens)").
		Order("id ASC").
		Find(&rows).Error
	return rows, err
}

// UpdateSharedAPIKeyLimits replaces both max input and max output token limits.
// A nil pointer means unlimited for that side. Zero is invalid.
func UpdateSharedAPIKeyLimits(ctx context.Context, db *gorm.DB, userID uint, publicID string, maxInput, maxOutput *uint64) (*models.SharedAPIKey, error) {
	if maxInput != nil && *maxInput == 0 {
		return nil, ErrSharedAPIKeyInvalidLimit
	}
	if maxOutput != nil && *maxOutput == 0 {
		return nil, ErrSharedAPIKeyInvalidLimit
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var row models.SharedAPIKey
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND public_id = ?", userID, publicID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharedAPIKeyNotFound
			}
			return err
		}
		if row.DeletedAt != nil {
			return ErrSharedAPIKeyDeleted
		}
		updates := map[string]interface{}{
			"max_input_tokens":  nil,
			"max_output_tokens": nil,
		}
		if maxInput != nil {
			updates["max_input_tokens"] = *maxInput
		}
		if maxOutput != nil {
			updates["max_output_tokens"] = *maxOutput
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&row, row.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ResetSharedAPIKeyUsage sets used input and output token counters to zero.
func ResetSharedAPIKeyUsage(ctx context.Context, db *gorm.DB, userID uint, publicID string) (*models.SharedAPIKey, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var row models.SharedAPIKey
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND public_id = ?", userID, publicID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharedAPIKeyNotFound
			}
			return err
		}
		if row.DeletedAt != nil {
			return ErrSharedAPIKeyDeleted
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"used_input_tokens":  uint64(0),
			"used_output_tokens": uint64(0),
		}).Error; err != nil {
			return err
		}
		row.UsedInputTokens = 0
		row.UsedOutputTokens = 0
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// AddSharedAPIKeyUsageTx locks the key row and adds settled input/output tokens.
func AddSharedAPIKeyUsageTx(tx *gorm.DB, keyID uint, inputTokens, outputTokens uint64) error {
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}
	var row models.SharedAPIKey
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, keyID).Error; err != nil {
		return err
	}
	return tx.Model(&row).Updates(map[string]interface{}{
		"used_input_tokens":  row.UsedInputTokens + inputTokens,
		"used_output_tokens": row.UsedOutputTokens + outputTokens,
	}).Error
}

// SelectRandomEligibleKey chooses one key uniformly at random after excluding full slots and tried IDs.
func SelectRandomEligibleKey(keys []models.SharedAPIKey, tried map[uint]struct{}, slots *ProviderKeySlotManager) (*models.SharedAPIKey, bool) {
	candidates := make([]models.SharedAPIKey, 0, len(keys))
	for _, key := range keys {
		if _, used := tried[key.ID]; used {
			continue
		}
		if slots != nil && !slots.Available(key.ID) {
			continue
		}
		candidates = append(candidates, key)
	}
	if len(candidates) == 0 {
		return nil, false
	}
	idx := rand.Intn(len(candidates))
	return &candidates[idx], true
}

// RecordSharedAPIKeyUnavailable atomically increments the consecutive unavailable count.
// On the third consecutive result it resets the count and starts cooldown.
func RecordSharedAPIKeyUnavailable(ctx context.Context, db *gorm.DB, keyID uint) error {
	cfg := config.GetConfig()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var row models.SharedAPIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, keyID).Error; err != nil {
			return err
		}
		next := row.ConsecutiveUnavailableCount + 1
		updates := map[string]interface{}{}
		if next >= 3 {
			cooldownUntil := time.Now().Add(time.Duration(cfg.Provider.KeyCooldownSeconds) * time.Second)
			updates["consecutive_unavailable_count"] = 0
			updates["cooldown_until"] = cooldownUntil
		} else {
			updates["consecutive_unavailable_count"] = next
		}
		return tx.Model(&row).Updates(updates).Error
	})
}

func ClearSharedAPIKeyUnavailable(ctx context.Context, db *gorm.DB, keyID uint) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Model(&models.SharedAPIKey{}).
		Where("id = ?", keyID).
		Updates(map[string]interface{}{
			"consecutive_unavailable_count": 0,
		}).Error
}

func DecryptSharedAPIKeySecret(key *models.SharedAPIKey) ([]byte, error) {
	enc := GetProviderKeyEncryption()
	if enc == nil {
		return nil, errors.New("provider key encryption is not initialized")
	}
	if key == nil || len(key.KeyCiphertext) == 0 || len(key.KeyNonce) == 0 {
		return nil, errors.New("shared api key secret is unavailable")
	}
	aad := SharedAPIKeyAAD(key.PublicID, key.UserID, string(key.ServiceProvider))
	return enc.Decrypt(key.KeyCiphertext, key.KeyNonce, aad)
}

func CountEnabledSharedAPIKeys(ctx context.Context, db *gorm.DB, userID uint) (int64, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var count int64
	err := db.WithContext(dbCtx).Model(&models.SharedAPIKey{}).
		Where("user_id = ? AND owner_enabled = ? AND deleted_at IS NULL", userID, true).
		Count(&count).Error
	return count, err
}
