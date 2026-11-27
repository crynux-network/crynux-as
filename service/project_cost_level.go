package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ProjectCostLevelUpdate holds optional Cost Level fields for a partial update.
type ProjectCostLevelUpdate struct {
	CostLevelMode         *string
	PriorityGwei          *string
	AutoQueuePosition     *int
	AutoMaxCreditsPerTask *string
}

// CostLevelFieldError is a field-scoped Cost Level validation failure.
type CostLevelFieldError struct {
	Field   string
	Message string
}

func (e *CostLevelFieldError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ProjectCostLevelView is the Cost Level subset returned to clients.
type ProjectCostLevelView struct {
	CostLevelMode         string  `json:"cost_level_mode"`
	PriorityGwei          string  `json:"priority_gwei"`
	AutoQueuePosition     *int    `json:"auto_queue_position"`
	AutoMaxCreditsPerTask *string `json:"auto_max_credits_per_task"`
}

// CostLevelHintsView is the Crynux Network Cost Level reasonableness snapshot.
type CostLevelHintsView struct {
	BaseVRAM                     uint64  `json:"base_vram"`
	CreditsPerGwei               string  `json:"credits_per_gwei"`
	MinPriorityGwei              string  `json:"min_priority_gwei"`
	MaxPriorityGwei              string  `json:"max_priority_gwei"`
	MedianPriorityGwei           string  `json:"median_priority_gwei"`
	HighestPriorityGwei          *string `json:"highest_priority_gwei"`
	LowestPriorityGwei           *string `json:"lowest_priority_gwei"`
	DefaultAutoMaxCreditsPerTask string  `json:"default_auto_max_credits_per_task"`
}

// ProjectCostLevelViewFromProject maps a project row to the Cost Level API view.
func ProjectCostLevelViewFromProject(project *models.Project) ProjectCostLevelView {
	view := ProjectCostLevelView{
		CostLevelMode: project.CostLevelMode,
		PriorityGwei:  project.PriorityGwei.String(),
	}
	if view.CostLevelMode == "" {
		view.CostLevelMode = models.CostLevelModeStatic
	}
	if project.AutoQueuePosition != nil {
		pos := *project.AutoQueuePosition
		view.AutoQueuePosition = &pos
	}
	if project.AutoMaxCreditsPerTask != nil {
		credits := project.AutoMaxCreditsPerTask.String()
		view.AutoMaxCreditsPerTask = &credits
	}
	return view
}

// ApplyProjectCostLevelUpdate validates and applies a partial Cost Level update
// to project in memory. The caller MUST persist the project afterward.
func ApplyProjectCostLevelUpdate(project *models.Project, in ProjectCostLevelUpdate) *CostLevelFieldError {
	if project == nil {
		return &CostLevelFieldError{Field: "body", Message: "project is required"}
	}
	if in.CostLevelMode == nil &&
		in.PriorityGwei == nil &&
		in.AutoQueuePosition == nil &&
		in.AutoMaxCreditsPerTask == nil {
		return &CostLevelFieldError{Field: "body", Message: "at least one field is required"}
	}

	if in.PriorityGwei != nil {
		parsed, err := ParsePriorityGwei(*in.PriorityGwei)
		if err != nil {
			return &CostLevelFieldError{Field: "priority_gwei", Message: err.Error()}
		}
		project.PriorityGwei = models.BigInt{Int: *parsed}
	}

	if in.AutoQueuePosition != nil {
		parsed, err := ParseAutoQueuePosition(*in.AutoQueuePosition)
		if err != nil {
			return &CostLevelFieldError{Field: "auto_queue_position", Message: err.Error()}
		}
		project.AutoQueuePosition = &parsed
	}

	if in.AutoMaxCreditsPerTask != nil {
		parsed, err := ParseAutoMaxCreditsPerTask(*in.AutoMaxCreditsPerTask)
		if err != nil {
			return &CostLevelFieldError{Field: "auto_max_credits_per_task", Message: err.Error()}
		}
		project.AutoMaxCreditsPerTask = &models.BigInt{Int: *parsed}
	}

	if in.CostLevelMode != nil {
		mode, err := ParseCostLevelMode(*in.CostLevelMode)
		if err != nil {
			return &CostLevelFieldError{Field: "cost_level_mode", Message: err.Error()}
		}
		if project.AutoMaxCreditsPerTask == nil || project.AutoMaxCreditsPerTask.Sign() <= 0 {
			return &CostLevelFieldError{
				Field:   "auto_max_credits_per_task",
				Message: "auto_max_credits_per_task is required",
			}
		}
		if mode == models.CostLevelModeAuto {
			if project.AutoQueuePosition == nil {
				pos := DefaultAutoQueuePosition
				project.AutoQueuePosition = &pos
			}
		}
		project.CostLevelMode = mode
	}
	return nil
}

// SaveProject persists project with a short DB timeout.
func SaveProject(ctx context.Context, db *gorm.DB, project *models.Project) error {
	if db == nil {
		return errors.New("database is required")
	}
	if project == nil {
		return errors.New("project is required")
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Save(project).Error
}

// ReloadProjectByID loads a project by primary key.
func ReloadProjectByID(ctx context.Context, db *gorm.DB, projectID uint) (*models.Project, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var project models.Project
	if err := db.WithContext(dbCtx).First(&project, projectID).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// BuildCostLevelHints builds the Crynux Network Cost Level reasonableness snapshot.
func BuildCostLevelHints() (*CostLevelHintsView, error) {
	llmCfg := config.GetConfig().LLM
	median, err := ResolveQueueMedianHint()
	if err != nil {
		return nil, err
	}
	defaultAutoMax, err := config.GetConfig().ParseDefaultAutoMaxCreditsPerTask()
	if err != nil {
		return nil, err
	}
	staticMin, staticMax, err := StaticProjectPriorityBounds()
	if err != nil {
		return nil, err
	}
	view := &CostLevelHintsView{
		BaseVRAM:                     llmCfg.BaseVRAM,
		CreditsPerGwei:               llmCfg.CreditsPerGwei,
		MinPriorityGwei:              staticMin.String(),
		MaxPriorityGwei:              staticMax.String(),
		MedianPriorityGwei:           median.String(),
		DefaultAutoMaxCreditsPerTask: defaultAutoMax.String(),
	}
	if highest, lowest, ok := ResolveQueuePriorityBounds(); ok {
		highestStr := highest.String()
		lowestStr := lowest.String()
		view.HighestPriorityGwei = &highestStr
		view.LowestPriorityGwei = &lowestStr
	}
	return view, nil
}

// UpdateProjectCostLevelAndSave applies a Cost Level update and persists the project.
func UpdateProjectCostLevelAndSave(
	ctx context.Context,
	db *gorm.DB,
	project *models.Project,
	in ProjectCostLevelUpdate,
) (*models.Project, *CostLevelFieldError, error) {
	if fieldErr := ApplyProjectCostLevelUpdate(project, in); fieldErr != nil {
		return nil, fieldErr, nil
	}
	if err := SaveProject(ctx, db, project); err != nil {
		return nil, nil, fmt.Errorf("save project cost level: %w", err)
	}
	reloaded, err := ReloadProjectByID(ctx, db, project.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("reload project after cost level update: %w", err)
	}
	return reloaded, nil, nil
}
