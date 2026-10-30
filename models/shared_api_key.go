package models

import "time"

type ServiceProvider string

const (
	ServiceProviderOpenAI    ServiceProvider = "openai"
	ServiceProviderAnthropic ServiceProvider = "anthropic"
	ServiceProviderGoogle    ServiceProvider = "google"
)

type ExecutionSource string

const (
	ExecutionSourceCrynux    ExecutionSource = "crynux"
	ExecutionSourceOpenAI    ExecutionSource = "openai"
	ExecutionSourceAnthropic ExecutionSource = "anthropic"
	ExecutionSourceGoogle    ExecutionSource = "google"
)

func (p ServiceProvider) ExecutionSource() ExecutionSource {
	switch p {
	case ServiceProviderOpenAI:
		return ExecutionSourceOpenAI
	case ServiceProviderAnthropic:
		return ExecutionSourceAnthropic
	case ServiceProviderGoogle:
		return ExecutionSourceGoogle
	default:
		return ""
	}
}

func (s ExecutionSource) IsProvider() bool {
	return s == ExecutionSourceOpenAI || s == ExecutionSourceAnthropic || s == ExecutionSourceGoogle
}

func (s ExecutionSource) ServiceProvider() ServiceProvider {
	switch s {
	case ExecutionSourceOpenAI:
		return ServiceProviderOpenAI
	case ExecutionSourceAnthropic:
		return ServiceProviderAnthropic
	case ExecutionSourceGoogle:
		return ServiceProviderGoogle
	default:
		return ""
	}
}

// SharedAPIKey stores one user-managed Shared API Key and its encrypted secret.
type SharedAPIKey struct {
	ID                          uint            `json:"id" gorm:"primarykey"`
	PublicID                    string          `json:"public_id" gorm:"type:string;size:64;not null;uniqueIndex"`
	CreatedAt                   time.Time       `json:"created_at" gorm:"not null"`
	UpdatedAt                   time.Time       `json:"updated_at" gorm:"not null"`
	UserID                      uint            `json:"user_id" gorm:"not null;index:idx_shared_api_keys_user_id,priority:1"`
	ServiceProvider             ServiceProvider `json:"service_provider" gorm:"type:string;size:32;not null;uniqueIndex:idx_shared_api_keys_provider_fingerprint,priority:1;index:idx_shared_api_keys_eligibility,priority:1"`
	MaskedSuffix                string          `json:"masked_suffix" gorm:"type:string;size:16;not null"`
	OwnerEnabled                bool            `json:"owner_enabled" gorm:"not null;default:true;index:idx_shared_api_keys_eligibility,priority:2"`
	DeletedAt                   *time.Time      `json:"deleted_at" gorm:"index:idx_shared_api_keys_eligibility,priority:3"`
	ConsecutiveUnavailableCount int             `json:"consecutive_unavailable_count" gorm:"not null;default:0"`
	CooldownUntil               *time.Time      `json:"cooldown_until" gorm:"index:idx_shared_api_keys_eligibility,priority:4"`
	UsedInputTokens             uint64          `json:"used_input_tokens" gorm:"not null;default:0"`
	UsedOutputTokens            uint64          `json:"used_output_tokens" gorm:"not null;default:0"`
	MaxInputTokens              *uint64         `json:"max_input_tokens"`
	MaxOutputTokens             *uint64         `json:"max_output_tokens"`
	Fingerprint                 *string         `json:"-" gorm:"type:string;size:64;uniqueIndex:idx_shared_api_keys_provider_fingerprint,priority:2"`
	KeyCiphertext               []byte          `json:"-" gorm:"type:blob"`
	KeyNonce                    []byte          `json:"-" gorm:"type:blob"`
}

func (SharedAPIKey) TableName() string {
	return "shared_api_keys"
}

func (k SharedAPIKey) IsDeleted() bool {
	return k.DeletedAt != nil
}
