package v1

import (
	"crynux_as/api/v1/account"
	"crynux_as/api/v1/auth"
	"crynux_as/api/v1/deposit"
	"crynux_as/api/v1/llm"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/projects"
	"crynux_as/api/v1/response"

	"github.com/loopfz/gadgeto/tonic"
	"github.com/wI2L/fizz"
)

func InitRoutes(r *fizz.Fizz) {
	v1g := r.Group("v1", "ApiV1", "API version 1")

	authGroup := v1g.Group("auth", "Auth", "Wallet login APIs")
	authGroup.POST("/login", []fizz.OperationOption{
		fizz.Summary("Login with a wallet signature and get a JWT token"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(auth.Login, 200))

	accountGroup := v1g.Group("account", "Account", "Account APIs", middleware.JWTAuthMiddleware())
	accountGroup.GET("", []fizz.OperationOption{
		fizz.Summary("Get the Credits balance of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetBalance, 200))
	accountGroup.GET("/deposits", []fizz.OperationOption{
		fizz.Summary("Get the deposit records of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetDeposits, 200))
	accountGroup.GET("/charges", []fizz.OperationOption{
		fizz.Summary("Get the Credits charge records of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetCharges, 200))

	depositGroup := v1g.Group("deposit", "Deposit", "Deposit configuration APIs", middleware.JWTAuthMiddleware())
	depositGroup.GET("/networks", []fizz.OperationOption{
		fizz.Summary("Get the configured deposit networks and tokens"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(deposit.GetNetworks, 200))

	projectsGroup := v1g.Group("projects", "Projects", "Project APIs", middleware.JWTAuthMiddleware())
	projectsGroup.POST("", []fizz.OperationOption{
		fizz.Summary("Create a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.CreateProject, 200))
	projectsGroup.GET("", []fizz.OperationOption{
		fizz.Summary("List the projects of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.ListProjects, 200))
	projectsGroup.GET("/:project_id", []fizz.OperationOption{
		fizz.Summary("Get a project by ID"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.GetProject, 200))
	projectsGroup.PUT("/:project_id", []fizz.OperationOption{
		fizz.Summary("Update a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.UpdateProject, 200))
	projectsGroup.DELETE("/:project_id", []fizz.OperationOption{
		fizz.Summary("Delete a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.DeleteProject, 200))

	projectsGroup.POST("/:project_id/api_key/reset", []fizz.OperationOption{
		fizz.Summary("Reset the API key of a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.ResetAPIKey, 200))

	projectsGroup.GET("/:project_id/stats", []fizz.OperationOption{
		fizz.Summary("Get the usage stats of a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.GetProjectStats, 200))

	llmGroup := v1g.Group("llm", "LLM", "LLM configuration APIs", middleware.JWTAuthMiddleware())
	llmGroup.GET("/billing_config", []fizz.OperationOption{
		fizz.Summary("Get the configured LLM billing reference priority and Credits conversion"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(llm.GetBillingConfig, 200))
	llmGroup.GET("/pricing_examples", []fizz.OperationOption{
		fizz.Summary("Get model coefficient rows for Credits and execution-time examples"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(llm.GetPricingExamples, 200))
}
