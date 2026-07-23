package projects

import (
	"crynux_as/api/v1/response"
	"errors"

	"github.com/gin-gonic/gin"
)

type APIKeyData struct {
	ID        uint   `json:"id" description:"The API key ID"`
	Prefix    string `json:"prefix" description:"The public prefix of the API key"`
	Status    int8   `json:"status" description:"The API key status"`
	CreatedAt int64  `json:"created_at" description:"The unix timestamp when the API key is created"`
}

type CreateAPIKeyInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

type CreateAPIKeyData struct {
	APIKeyData
	Key string `json:"key" description:"The plaintext API key. Returned only once at creation"`
}

type CreateAPIKeyResponse struct {
	response.Response
	Data *CreateAPIKeyData `json:"data"`
}

func CreateAPIKey(_ *gin.Context, _ *CreateAPIKeyInput) (*CreateAPIKeyResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type ListAPIKeysInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

type ListAPIKeysData struct {
	APIKeys []APIKeyData `json:"api_keys" description:"The API keys of the project"`
}

type ListAPIKeysResponse struct {
	response.Response
	Data *ListAPIKeysData `json:"data"`
}

func ListAPIKeys(_ *gin.Context, _ *ListAPIKeysInput) (*ListAPIKeysResponse, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}

type DeleteAPIKeyInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
	APIKeyID  uint `path:"api_key_id" validate:"required" description:"The API key ID"`
}

func DeleteAPIKey(_ *gin.Context, _ *DeleteAPIKeyInput) (*response.Response, error) {
	return nil, response.NewExceptionResponse(errors.New("not implemented"))
}
