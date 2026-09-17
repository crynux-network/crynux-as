package projects

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"crynux_as/utils"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ProjectData struct {
	ID                        uint    `json:"id" description:"The project ID"`
	Name                      string  `json:"name" description:"The project name"`
	EndpointToken             string  `json:"endpoint_token" description:"The unique token in the private LLM API base URL of the project"`
	APIKeyPrefix              string  `json:"api_key_prefix" description:"The public prefix of the project API key"`
	CostLevelMode             string  `json:"cost_level_mode" description:"Cost Level mode: static or auto"`
	PriorityGwei              string  `json:"priority_gwei" description:"Static Cost Level priority in Gwei"`
	AutoQueuePosition         *int    `json:"auto_queue_position" description:"Auto Cost Level queue position from 0 to 100"`
	AutoMaxPriorityGwei       *string `json:"auto_max_priority_gwei" description:"Auto Cost Level max priority in Gwei"`
	Status                    int8    `json:"status" description:"The project status"`
	CreatedAt                 int64   `json:"created_at" description:"The unix timestamp when the project is created"`
	LastRequestAt             *int64  `json:"last_request_at" description:"Unix timestamp of the latest finished request that entered usage stats"`
	RequestCountDay           uint64  `json:"request_count_day" description:"Finished request count for the current Unix day"`
	SuccessCountDay           uint64  `json:"success_count_day" description:"Successful request count for the current Unix day"`
	FailureCountDay           uint64  `json:"failure_count_day" description:"Failed request count for the current Unix day"`
	CreditsDay                string  `json:"credits_day" description:"Credits charged for the current Unix day"`
	RecentWindowSuccessCount  uint64  `json:"recent_window_success_count" description:"Successful request count in the configured recent failure window"`
	RecentWindowFailureCount  uint64  `json:"recent_window_failure_count" description:"Failed request count in the configured recent failure window"`
	ElevatedRecentFailureRate bool    `json:"elevated_recent_failure_rate" description:"True when recent-window failure rate is strictly above the configured threshold"`
}

type ProjectResponse struct {
	response.Response
	Data *ProjectData `json:"data"`
}

type CreateProjectInput struct {
	Name string `json:"name" validate:"required" description:"The project name"`
}

type CreateProjectData struct {
	ProjectData
	APIKey string `json:"api_key" description:"The plaintext API key. Returned only once at creation"`
}

type CreateProjectResponse struct {
	response.Response
	Data *CreateProjectData `json:"data"`
}

func CreateProject(c *gin.Context, in *CreateProjectInput) (*CreateProjectResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	endpointToken, err := utils.GenerateRandomToken(32)
	if err != nil {
		log.Errorf("Error generating endpoint token: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	apiKey, err := utils.GenerateRandomToken(32)
	if err != nil {
		log.Errorf("Error generating API key: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	priorityGwei, err := service.InitialProjectPriorityGwei()
	if err != nil {
		log.Errorf("Error resolving initial project priority: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	autoMaxPriorityGwei, err := service.InitialProjectAutoMaxPriorityGwei()
	if err != nil {
		log.Errorf("Error resolving initial auto max priority: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	autoQueuePosition := service.DefaultAutoQueuePosition

	project := models.Project{
		UserID:              user.ID,
		Name:                in.Name,
		EndpointToken:       endpointToken,
		APIKeyHash:          utils.HashToken(apiKey),
		APIKeyPrefix:        apiKeyPrefix(apiKey),
		CostLevelMode:       models.CostLevelModeStatic,
		PriorityGwei:        models.BigInt{Int: *priorityGwei},
		AutoQueuePosition:   &autoQueuePosition,
		AutoMaxPriorityGwei: &models.BigInt{Int: *autoMaxPriorityGwei},
		Status:              models.ProjectStatusActive,
	}

	db := config.GetDB()
	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := db.WithContext(dbCtx).Create(&project).Error; err != nil {
		log.Errorf("Error creating project for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &CreateProjectResponse{
		Data: &CreateProjectData{
			ProjectData: toProjectData(&project),
			APIKey:      apiKey,
		},
	}, nil
}

type ListProjectsData struct {
	Projects []ProjectData `json:"projects" description:"The projects of the account"`
}

type ListProjectsResponse struct {
	response.Response
	Data *ListProjectsData `json:"data"`
}

func ListProjects(c *gin.Context) (*ListProjectsResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	db := config.GetDB()
	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var projects []models.Project
	if err := db.WithContext(dbCtx).
		Where("user_id = ? AND status <> ?", user.ID, models.ProjectStatusDeleted).
		Order("id ASC").
		Find(&projects).Error; err != nil {
		log.Errorf("Error listing projects for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	items := make([]ProjectData, 0, len(projects))
	for i := range projects {
		items = append(items, toProjectData(&projects[i]))
	}

	return &ListProjectsResponse{
		Data: &ListProjectsData{Projects: items},
	}, nil
}

type GetProjectInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

func GetProject(c *gin.Context, in *GetProjectInput) (*ProjectResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	project, err := findOwnedProject(c.Request.Context(), config.GetDB(), user.ID, in.ProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	data := toProjectData(project)
	return &ProjectResponse{Data: &data}, nil
}

type UpdateProjectInput struct {
	ProjectID           uint    `path:"project_id" validate:"required" description:"The project ID"`
	Name                *string `json:"name" description:"The new project name"`
	CostLevelMode       *string `json:"cost_level_mode" description:"Cost Level mode: static or auto"`
	PriorityGwei        *string `json:"priority_gwei" description:"Static Cost Level priority in Gwei"`
	AutoQueuePosition   *int    `json:"auto_queue_position" description:"Auto Cost Level queue position from 0 to 100"`
	AutoMaxPriorityGwei *string `json:"auto_max_priority_gwei" description:"Auto Cost Level max priority in Gwei"`
}

func UpdateProject(c *gin.Context, in *UpdateProjectInput) (*ProjectResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	if in.Name == nil &&
		in.CostLevelMode == nil &&
		in.PriorityGwei == nil &&
		in.AutoQueuePosition == nil &&
		in.AutoMaxPriorityGwei == nil {
		return nil, response.NewValidationErrorResponse("body", "at least one field is required")
	}

	db := config.GetDB()
	project, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	if in.Name != nil {
		name := *in.Name
		if name == "" {
			return nil, response.NewValidationErrorResponse("name", "name is required")
		}
		project.Name = name
	}

	if in.PriorityGwei != nil {
		parsed, err := service.ParsePriorityGwei(*in.PriorityGwei)
		if err != nil {
			return nil, response.NewValidationErrorResponse("priority_gwei", err.Error())
		}
		project.PriorityGwei = models.BigInt{Int: *parsed}
	}

	if in.AutoQueuePosition != nil {
		parsed, err := service.ParseAutoQueuePosition(*in.AutoQueuePosition)
		if err != nil {
			return nil, response.NewValidationErrorResponse("auto_queue_position", err.Error())
		}
		project.AutoQueuePosition = &parsed
	}

	if in.AutoMaxPriorityGwei != nil {
		parsed, err := service.ParsePriorityGwei(*in.AutoMaxPriorityGwei)
		if err != nil {
			return nil, response.NewValidationErrorResponse("auto_max_priority_gwei", err.Error())
		}
		project.AutoMaxPriorityGwei = &models.BigInt{Int: *parsed}
	}

	if in.CostLevelMode != nil {
		mode, err := service.ParseCostLevelMode(*in.CostLevelMode)
		if err != nil {
			return nil, response.NewValidationErrorResponse("cost_level_mode", err.Error())
		}
		if mode == models.CostLevelModeAuto {
			if project.AutoMaxPriorityGwei == nil || project.AutoMaxPriorityGwei.Sign() <= 0 {
				return nil, response.NewValidationErrorResponse(
					"auto_max_priority_gwei",
					"auto_max_priority_gwei is required for auto mode",
				)
			}
			if project.AutoQueuePosition == nil {
				pos := service.DefaultAutoQueuePosition
				project.AutoQueuePosition = &pos
			}
		}
		project.CostLevelMode = mode
	}

	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := db.WithContext(dbCtx).Save(project).Error; err != nil {
		log.Errorf("Error updating project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	data := toProjectData(project)
	return &ProjectResponse{Data: &data}, nil
}

type DeleteProjectInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

func DeleteProject(c *gin.Context, in *DeleteProjectInput) (*response.Response, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	db := config.GetDB()
	project, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	project.Status = models.ProjectStatusDeleted
	if err := db.WithContext(dbCtx).Save(project).Error; err != nil {
		log.Errorf("Error deleting project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &response.Response{}, nil
}

type ResetAPIKeyInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

type ResetAPIKeyData struct {
	APIKeyPrefix string `json:"api_key_prefix" description:"The public prefix of the project API key"`
	APIKey       string `json:"api_key" description:"The plaintext API key. Returned only once at reset"`
}

type ResetAPIKeyResponse struct {
	response.Response
	Data *ResetAPIKeyData `json:"data"`
}

func ResetAPIKey(c *gin.Context, in *ResetAPIKeyInput) (*ResetAPIKeyResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	db := config.GetDB()
	project, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for user %d: %v", in.ProjectID, user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	apiKey, err := utils.GenerateRandomToken(32)
	if err != nil {
		log.Errorf("Error generating API key for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	project.APIKeyHash = utils.HashToken(apiKey)
	project.APIKeyPrefix = apiKeyPrefix(apiKey)
	if err := db.WithContext(dbCtx).Save(project).Error; err != nil {
		log.Errorf("Error resetting API key for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	return &ResetAPIKeyResponse{
		Data: &ResetAPIKeyData{
			APIKeyPrefix: project.APIKeyPrefix,
			APIKey:       apiKey,
		},
	}, nil
}

func currentUser(c *gin.Context) (*models.User, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}

	user, err := models.FindUserByAddress(c.Request.Context(), config.GetDB(), address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user: %v", err)
		return nil, response.NewExceptionResponse(err)
	}
	return user, nil
}

func findOwnedProject(ctx context.Context, db *gorm.DB, userID, projectID uint) (*models.Project, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var project models.Project
	if err := db.WithContext(dbCtx).
		Where("id = ? AND user_id = ? AND status <> ?", projectID, userID, models.ProjectStatusDeleted).
		First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func toProjectData(p *models.Project) ProjectData {
	mode := p.CostLevelMode
	if mode == "" {
		mode = models.CostLevelModeStatic
	}
	elevated := false
	if cfg := config.GetConfig(); cfg != nil {
		if threshold, err := cfg.ParseRecentFailureRateThreshold(); err == nil {
			elevated = service.ElevatedRecentFailureRate(
				p.RecentWindowSuccessCount,
				p.RecentWindowFailureCount,
				threshold,
			)
		}
	}
	data := ProjectData{
		ID:                        p.ID,
		Name:                      p.Name,
		EndpointToken:             p.EndpointToken,
		APIKeyPrefix:              p.APIKeyPrefix,
		CostLevelMode:             mode,
		PriorityGwei:              p.PriorityGwei.String(),
		AutoQueuePosition:         p.AutoQueuePosition,
		Status:                    int8(p.Status),
		CreatedAt:                 p.CreatedAt.Unix(),
		LastRequestAt:             p.LastRequestAt,
		RequestCountDay:           p.RequestCountDay,
		SuccessCountDay:           p.SuccessCountDay,
		FailureCountDay:           p.FailureCountDay,
		CreditsDay:                p.CreditsDay.String(),
		RecentWindowSuccessCount:  p.RecentWindowSuccessCount,
		RecentWindowFailureCount:  p.RecentWindowFailureCount,
		ElevatedRecentFailureRate: elevated,
	}
	if p.AutoMaxPriorityGwei != nil {
		value := p.AutoMaxPriorityGwei.String()
		data.AutoMaxPriorityGwei = &value
	}
	return data
}

func apiKeyPrefix(apiKey string) string {
	if len(apiKey) < 8 {
		return apiKey
	}
	return apiKey[:8]
}
