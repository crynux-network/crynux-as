package v1

import (
	"crynux_as/api/v1/account"
	"crynux_as/api/v1/auth"
	"crynux_as/api/v1/earn"
	"crynux_as/api/v1/llm"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/projects"
	"crynux_as/api/v1/purchase"
	"crynux_as/api/v1/response"
	"crynux_as/api/v1/wallet"

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
	accountGroup.GET("/purchases", []fizz.OperationOption{
		fizz.Summary("Get the purchase and bonus credit records of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetPurchases, 200))
	accountGroup.GET("/referral", []fizz.OperationOption{
		fizz.Summary("Get the referral settings and stats of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetReferral, 200))
	accountGroup.POST("/referral/code/reset", []fizz.OperationOption{
		fizz.Summary("Reset the active invite code of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.ResetReferralCode, 200))
	accountGroup.GET("/referral/rewards", []fizz.OperationOption{
		fizz.Summary("Get the referral rewards earned by the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetReferralRewards, 200))
	accountGroup.GET("/charges", []fizz.OperationOption{
		fizz.Summary("Get the Credits charge records of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetCharges, 200))
	accountGroup.GET("/stats", []fizz.OperationOption{
		fizz.Summary("Get the usage stats of the account"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(account.GetAccountStats, 200))

	purchaseGroup := v1g.Group("purchase", "Purchase", "Purchase configuration APIs", middleware.JWTAuthMiddleware())
	purchaseGroup.GET("/networks", []fizz.OperationOption{
		fizz.Summary("Get the configured purchase networks and tokens"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(purchase.GetNetworks, 200))

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
	projectsGroup.GET("/:project_id/stats/completion-duration", []fizz.OperationOption{
		fizz.Summary("Get the completion duration histogram of a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.GetProjectCompletionDurationStats, 200))
	projectsGroup.GET("/:project_id/stats/models", []fizz.OperationOption{
		fizz.Summary("Get the top model usage stats of a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.GetProjectModelStats, 200))
	projectsGroup.GET("/:project_id/requests", []fizz.OperationOption{
		fizz.Summary("Get the recent LLM call records of a project"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("404", "project not found", response.NotFoundErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(projects.GetProjectRequests, 200))

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
	llmGroup.GET("/image_pricing_examples", []fizz.OperationOption{
		fizz.Summary("Get Image model coefficient rows for Credits and execution-time examples"),
		fizz.Response("400", "validation errors", response.ValidationErrorResponse{}, nil, nil),
		fizz.Response("500", "exception", response.ExceptionResponse{}, nil, nil),
	}, tonic.Handler(llm.GetImagePricingExamples, 200))

	earnGroup := v1g.Group("earn", "Earn", "Provider Key Sharing Earn APIs", middleware.JWTAuthMiddleware())
	earnGroup.GET("", []fizz.OperationOption{fizz.Summary("Get Key Owner earning summary")}, tonic.Handler(earn.GetSummary, 200))
	earnGroup.GET("/providers", []fizz.OperationOption{fizz.Summary("List configured Service Providers")}, tonic.Handler(earn.ListProviders, 200))
	earnGroup.GET("/keys", []fizz.OperationOption{fizz.Summary("List Shared API Keys")}, tonic.Handler(earn.ListKeys, 200))
	earnGroup.POST("/keys", []fizz.OperationOption{fizz.Summary("Create a Shared API Key")}, tonic.Handler(earn.CreateKey, 200))
	earnGroup.POST("/keys/:key_id/enable", []fizz.OperationOption{fizz.Summary("Enable a Shared API Key")}, tonic.Handler(earn.EnableKey, 200))
	earnGroup.POST("/keys/:key_id/disable", []fizz.OperationOption{fizz.Summary("Disable a Shared API Key")}, tonic.Handler(earn.DisableKey, 200))
	earnGroup.DELETE("/keys/:key_id", []fizz.OperationOption{fizz.Summary("Delete a Shared API Key")}, tonic.Handler(earn.DeleteKey, 200))
	earnGroup.POST("/keys/:key_id/limits", []fizz.OperationOption{fizz.Summary("Set Shared API Key token limits")}, tonic.Handler(earn.SetKeyLimits, 200))
	earnGroup.POST("/keys/:key_id/usage/reset", []fizz.OperationOption{fizz.Summary("Reset Shared API Key usage counters")}, tonic.Handler(earn.ResetKeyUsage, 200))
	earnGroup.GET("/calls", []fizz.OperationOption{fizz.Summary("List Key Owner call records")}, tonic.Handler(earn.ListCalls, 200))
	earnGroup.GET("/events", []fizz.OperationOption{fizz.Summary("List TaskIncome earning events")}, tonic.Handler(earn.ListEvents, 200))
	earnGroup.GET("/withdrawal_networks", []fizz.OperationOption{fizz.Summary("List withdrawal networks")}, tonic.Handler(earn.ListWithdrawalNetworks, 200))
	earnGroup.POST("/withdrawals/prepare", []fizz.OperationOption{fizz.Summary("Prepare a withdrawal authorization message")}, tonic.Handler(earn.PrepareWithdrawal, 200))
	earnGroup.POST("/withdrawals", []fizz.OperationOption{fizz.Summary("Create a withdrawal")}, tonic.Handler(earn.CreateWithdrawal, 200))
	earnGroup.GET("/withdrawals", []fizz.OperationOption{fizz.Summary("List withdrawals")}, tonic.Handler(earn.ListWithdrawals, 200))

	v1g.GET("/relay_account/event_logs", []fizz.OperationOption{fizz.Summary("Wallet event logs")}, tonic.Handler(wallet.GetEventLogs, 200))
	v1g.GET("/withdraw/list", []fizz.OperationOption{fizz.Summary("Wallet pending withdrawals")}, tonic.Handler(wallet.GetWithdrawList, 200))
	v1g.POST("/withdraw/:id/fulfill", []fizz.OperationOption{fizz.Summary("Fulfill a withdrawal")}, tonic.Handler(wallet.FulfillWithdrawal, 200))
	v1g.POST("/withdraw/:id/reject", []fizz.OperationOption{fizz.Summary("Reject a withdrawal")}, tonic.Handler(wallet.RejectWithdrawal, 200))
}
