package projects

import (
	"crynux_as/api/v1/response"
	"errors"

	"github.com/gin-gonic/gin"
)

type ProjectData struct {
	ID            uint   `json:"id" description:"The project ID"`
	Name          string `json:"name" description:"The project name"`
	EndpointToken string `json:"endpoint_token" description:"The unique token in the private LLM API base URL of the project"`
	Status        int8   `json:"status" description:"The project status"`
	CreatedAt     int64  `json:"created_at" description:"The unix timestamp when the project is created"`
}

type ProjectResponse struct {
	response.Response
	Data *ProjectData `json:"data"`
}

type CreateProjectInput struct {
	Name string `json:"name" validate:"required" description:"The project name"`
}

func CreateProject(_ *gin.Context, _ *CreateProjectInput) (*ProjectResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type ListProjectsData struct {
	Projects []ProjectData `json:"projects" description:"The projects of the account"`
}

type ListProjectsResponse struct {
	response.Response
	Data *ListProjectsData `json:"data"`
}

func ListProjects(_ *gin.Context) (*ListProjectsResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type GetProjectInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

func GetProject(_ *gin.Context, _ *GetProjectInput) (*ProjectResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type UpdateProjectInput struct {
	ProjectID uint   `path:"project_id" validate:"required" description:"The project ID"`
	Name      string `json:"name" validate:"required" description:"The new project name"`
}

func UpdateProject(_ *gin.Context, _ *UpdateProjectInput) (*ProjectResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type DeleteProjectInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

func DeleteProject(_ *gin.Context, _ *DeleteProjectInput) (*response.Response, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
