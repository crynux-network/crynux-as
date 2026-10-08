package models

import (
	"encoding/json"
	"errors"
	"math/big"
	"time"
)

type TaskType int8

const (
	TaskTypeImage TaskType = iota
	TaskTypeLLM
)

type TaskAPIType int8

const (
	TaskAPITypeRaw TaskAPIType = iota
	TaskAPITypeResponses
	TaskAPITypeChatCompletions
	TaskAPITypeCompletions
)

type TaskJobStatus int8

const (
	TaskJobStatusPendingSubmit TaskJobStatus = iota
	TaskJobStatusSubmitted
	TaskJobStatusInProgress
	TaskJobStatusCompleted
	TaskJobStatusFailed
)

type TaskJobBillingStatus int8

const (
	TaskJobBillingPending TaskJobBillingStatus = iota
	TaskJobBillingBilled
	TaskJobBillingNotBilled
)

type TaskJob struct {
	ID                    uint        `gorm:"primarykey"`
	CreatedAt             time.Time   `gorm:"not null;index:idx_task_jobs_project_status_created,priority:3;index"`
	UpdatedAt             time.Time   `gorm:"not null"`
	ProjectID             uint        `gorm:"not null;index:idx_task_jobs_project_status_created,priority:1;index:idx_task_jobs_project_public_id,priority:1;index"`
	UserID                uint        `gorm:"not null;index"`
	PriorityGwei          BigInt      `gorm:"type:string;size:255;not null"`
	TaskType              TaskType    `gorm:"not null"`
	APIType               TaskAPIType `gorm:"not null"`
	Model                 string      `gorm:"type:string;size:255;not null"`
	BilledVram            uint64      `gorm:"not null;default:0"`
	PublicID              string      `gorm:"type:string;size:128;index:idx_task_jobs_project_public_id,priority:2;index"`
	Background            bool        `gorm:"not null;default:false"`
	Stream                bool        `gorm:"not null;default:false"`
	TaskArgsJSON          string      `gorm:"type:longtext;not null"`
	TaskVersion           *string     `gorm:"type:string;size:64"`
	MinVram               *uint64
	RequiredGPU           string               `gorm:"type:string;size:255"`
	RequiredGPUVram       uint64               `gorm:"not null;default:0"`
	RepeatNum             uint64               `gorm:"not null;default:1"`
	BridgeClientTaskID    *uint                `gorm:"index"`
	Status                TaskJobStatus        `gorm:"not null;default:0;index:idx_task_jobs_project_status_created,priority:2;index:idx_task_jobs_terminal_cleanup,priority:1;index"`
	RawResultJSON         *string              `gorm:"type:longtext"`
	FormattedResultJSON   *string              `gorm:"type:longtext"`
	ErrorMessage          *string              `gorm:"type:text"`
	BillingStatus         TaskJobBillingStatus `gorm:"not null;default:0;index:idx_task_jobs_terminal_cleanup,priority:2;index"`
	TaskCallRecordID      *uint                `gorm:"index"`
	BillingData           string               `gorm:"type:longtext;not null"`
	RequestBody           []byte               `gorm:"-"`
	TaskFeeGwei           *BigInt              `gorm:"-"`
	MedianPriorityGwei    *BigInt              `gorm:"-"`
	EstimatedNodeSeconds  *float64             `gorm:"-"`
	VramWeight            *float64             `gorm:"-"`
	ConstantSeconds       *float64             `gorm:"-"`
	SecondsPerInputToken  *float64             `gorm:"-"`
	SecondsPerOutputToken *float64             `gorm:"-"`
	StartedAt             *time.Time
	CompletedAt           *time.Time `gorm:"index:idx_task_jobs_terminal_cleanup,priority:3"`
}

const TaskBillingDataVersion = 1

type TaskBillingData struct {
	Version      int                   `json:"version"`
	PriorityGwei string                `json:"priority_gwei"`
	BilledVram   uint64                `json:"billed_vram"`
	TaskFeeWei   string                `json:"task_fee_wei"`
	LLM          *LLMTaskBillingData   `json:"llm,omitempty"`
	Image        *ImageTaskBillingData `json:"image,omitempty"`
}

type LLMTaskBillingData struct {
	VramWeight            float64 `json:"vram_weight"`
	ConstantSeconds       float64 `json:"constant_seconds"`
	SecondsPerInputToken  float64 `json:"seconds_per_input_token"`
	SecondsPerOutputToken float64 `json:"seconds_per_output_token"`
	CreditsPerGwei        string  `json:"credits_per_gwei"`
}

type ImageTaskBillingData struct {
	SettlementCredits string `json:"settlement_credits"`
}

func (j TaskJob) DecodeBillingData() (*TaskBillingData, error) {
	var data TaskBillingData
	if j.BillingData == "" && j.PriorityGwei.Sign() > 0 {
		taskFeeWei := "0"
		if j.TaskFeeGwei != nil {
			taskFeeWei = new(big.Int).Mul(&j.TaskFeeGwei.Int, big.NewInt(1_000_000_000)).String()
		}
		data = TaskBillingData{
			Version:      TaskBillingDataVersion,
			PriorityGwei: j.PriorityGwei.String(),
			BilledVram:   j.BilledVram,
			TaskFeeWei:   taskFeeWei,
		}
		if j.TaskType == TaskTypeLLM && j.VramWeight != nil && j.ConstantSeconds != nil &&
			j.SecondsPerInputToken != nil && j.SecondsPerOutputToken != nil {
			data.LLM = &LLMTaskBillingData{
				VramWeight:            *j.VramWeight,
				ConstantSeconds:       *j.ConstantSeconds,
				SecondsPerInputToken:  *j.SecondsPerInputToken,
				SecondsPerOutputToken: *j.SecondsPerOutputToken,
				CreditsPerGwei:        "1",
			}
		}
		if data.LLM == nil && j.VramWeight != nil && j.ConstantSeconds != nil &&
			j.SecondsPerInputToken != nil && j.SecondsPerOutputToken != nil {
			data.LLM = &LLMTaskBillingData{
				VramWeight:            *j.VramWeight,
				ConstantSeconds:       *j.ConstantSeconds,
				SecondsPerInputToken:  *j.SecondsPerInputToken,
				SecondsPerOutputToken: *j.SecondsPerOutputToken,
				CreditsPerGwei:        "1",
			}
		}
		return &data, nil
	} else if err := json.Unmarshal([]byte(j.BillingData), &data); err != nil {
		return nil, err
	}
	if data.Version != TaskBillingDataVersion {
		return nil, errors.New("unsupported billing data version")
	}
	if value, ok := new(big.Int).SetString(data.PriorityGwei, 10); !ok || value.Sign() <= 0 {
		return nil, errors.New("invalid billing priority_gwei")
	}
	if value, ok := new(big.Int).SetString(data.TaskFeeWei, 10); !ok || value.Sign() < 0 {
		return nil, errors.New("invalid billing task_fee_wei")
	}
	if data.BilledVram == 0 {
		return nil, errors.New("invalid billing billed_vram")
	}
	if j.TaskType == TaskTypeLLM && data.LLM == nil {
		return nil, errors.New("missing llm billing data")
	}
	if j.TaskType == TaskTypeImage && data.Image == nil {
		return nil, errors.New("missing image billing data")
	}
	return &data, nil
}

func EncodeTaskBillingData(data TaskBillingData) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (TaskJob) TableName() string {
	return "task_jobs"
}

func (j TaskJob) IsTerminal() bool {
	return j.Status == TaskJobStatusCompleted || j.Status == TaskJobStatusFailed
}
