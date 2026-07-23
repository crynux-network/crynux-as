package projects

import (
	"crynux_as/api/v1/response"
	"crynux_as/models"
	"errors"

	"github.com/gin-gonic/gin"
)

type GetProjectStatsInput struct {
	ProjectID uint  `path:"project_id" validate:"required" description:"The project ID"`
	StartTime int64 `query:"start_time" description:"The unix timestamp of the start of the stats period"`
	EndTime   int64 `query:"end_time" description:"The unix timestamp of the end of the stats period"`
}

type GetProjectStatsData struct {
	Stats []models.ProjectUsageStat `json:"stats" description:"The aggregated usage stats of the project"`
}

type GetProjectStatsResponse struct {
	response.Response
	Data *GetProjectStatsData `json:"data"`
}

func GetProjectStats(_ *gin.Context, _ *GetProjectStatsInput) (*GetProjectStatsResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
