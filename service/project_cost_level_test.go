package service

import (
	"crynux_as/models"
	"math/big"
	"testing"
)

func TestApplyProjectCostLevelUpdateRequiresField(t *testing.T) {
	project := &models.Project{
		CostLevelMode: models.CostLevelModeStatic,
		PriorityGwei:  models.BigInt{Int: *big.NewInt(1000)},
	}
	if err := ApplyProjectCostLevelUpdate(project, ProjectCostLevelUpdate{}); err == nil {
		t.Fatal("expected at least one field required")
	}
}

func TestApplyProjectCostLevelUpdateSetsAutoDefaults(t *testing.T) {
	maxCredits := big.NewInt(100)
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		PriorityGwei:          models.BigInt{Int: *big.NewInt(1000)},
		AutoMaxCreditsPerTask: &models.BigInt{Int: *maxCredits},
	}
	mode := models.CostLevelModeAuto
	if err := ApplyProjectCostLevelUpdate(project, ProjectCostLevelUpdate{
		CostLevelMode: &mode,
	}); err != nil {
		t.Fatalf("ApplyProjectCostLevelUpdate: %v", err)
	}
	if project.CostLevelMode != models.CostLevelModeAuto {
		t.Fatalf("mode = %s", project.CostLevelMode)
	}
	if project.AutoQueuePosition == nil || *project.AutoQueuePosition != DefaultAutoQueuePosition {
		t.Fatalf("auto_queue_position = %#v", project.AutoQueuePosition)
	}
}

func TestProjectCostLevelViewFromProject(t *testing.T) {
	pos := 40
	credits := big.NewInt(200)
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		PriorityGwei:          models.BigInt{Int: *big.NewInt(5000)},
		AutoQueuePosition:     &pos,
		AutoMaxCreditsPerTask: &models.BigInt{Int: *credits},
	}
	view := ProjectCostLevelViewFromProject(project)
	if view.CostLevelMode != models.CostLevelModeAuto ||
		view.PriorityGwei != "5000" ||
		view.AutoQueuePosition == nil ||
		*view.AutoQueuePosition != 40 ||
		view.AutoMaxCreditsPerTask == nil ||
		*view.AutoMaxCreditsPerTask != "200" {
		t.Fatalf("unexpected view: %+v", view)
	}
}
