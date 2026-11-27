package mcp

import (
	"context"
	"crynux_as/api/llm"
	"crynux_as/api/raw_task"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type contextKey string

const projectContextKey contextKey = "mcp_project"

func InitRoutes(engine *gin.Engine) {
	mcpServer := newProjectMCPServer()
	httpServer := mcpserver.NewStreamableHTTPServer(mcpServer)
	group := engine.Group("/api/:endpoint_token/mcp")
	group.Use(llm.ProjectAuthMiddleware())
	group.Use(func(c *gin.Context) {
		projectFromGin(c)
		c.Next()
	})
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		group.Handle(method, "", gin.WrapH(httpServer))
		group.Handle(method, "/*path", gin.WrapH(httpServer))
	}
}

func projectFromGin(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if project := llm.GetProject(c); project != nil {
		ctx = context.WithValue(ctx, projectContextKey, project)
	}
	c.Request = c.Request.WithContext(ctx)
	return ctx
}

func getProject(ctx context.Context) (*models.Project, error) {
	project, _ := ctx.Value(projectContextKey).(*models.Project)
	if project == nil {
		return nil, errors.New("project is not authenticated")
	}
	return project, nil
}

const createTaskGuideHint = "Before calling, read resource crynux://guides/raw-task for usage (model selection, model_not_ready, results, Cost Level). "

func newProjectMCPServer() *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"crynux-as",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithPromptCapabilities(true),
		mcpserver.WithRecovery(),
	)

	s.AddTool(mcp.NewTool(
		"list_models",
		mcp.WithDescription("List Crynux AS models. catalog may be all, chat, image, or video. Prefer rows with on_disk_node_count >= 1 and copy one entire row's dtype/quantization_config/components/lora into TaskArgs."),
		mcp.WithString("catalog", mcp.Description("Optional catalog filter: all, chat, image, or video. Default all.")),
	), handleListModels)

	s.AddTool(mcp.NewTool(
		"create_task",
		mcp.WithDescription(createTaskGuideHint+"Create one Raw Task. task_type is 0 Image, 1 LLM, or 3 Video. model_not_ready responses still include client_task_id and the task keeps running—continue with get_task. On success, Image tasks include result_download_urls and Video tasks include result_download_url. Remote MCP has no image/video save tools; install the local MCP package for bulk local save."),
		mcp.WithString("task_args", mcp.Required(), mcp.Description("TaskArgs JSON string for the selected task_type.")),
		mcp.WithNumber("task_type", mcp.Required(), mcp.Description("0 Image, 1 LLM, or 3 Video.")),
		mcp.WithNumber("min_vram", mcp.Description("Optional minimum VRAM in GB.")),
		mcp.WithNumber("queue_timeout", mcp.Description("Optional queue timeout in seconds.")),
	), handleCreateTask)

	s.AddTool(mcp.NewTool(
		"batch_create_tasks",
		mcp.WithDescription(createTaskGuideHint+"Create 1 to 100 Raw Tasks in one call. Same task fields as create_task per item. Items may return model_not_ready with client_task_id while the task keeps running."),
		mcp.WithString("tasks_json", mcp.Required(), mcp.Description("JSON array of create task objects with task_args, task_type, and optional min_vram and queue_timeout.")),
	), handleBatchCreateTasks)

	s.AddTool(mcp.NewTool(
		"get_task",
		mcp.WithDescription("Get one Raw Task status view by client_task_id. Use after create, including after model_not_ready. On Image/Video success the view includes result_download_urls or result_download_url."),
		mcp.WithNumber("client_task_id", mcp.Required(), mcp.Description("Local task_jobs.id.")),
	), handleGetTask)

	s.AddTool(mcp.NewTool(
		"batch_get_task_status",
		mcp.WithDescription("Get status for 1 to 100 Raw Tasks."),
		mcp.WithString("client_task_ids_json", mcp.Required(), mcp.Description("JSON array of client_task_id numbers.")),
	), handleBatchGetTaskStatus)

	s.AddTool(mcp.NewTool(
		"abort_task",
		mcp.WithDescription("Abort one Raw Task by client_task_id."),
		mcp.WithNumber("client_task_id", mcp.Required(), mcp.Description("Local task_jobs.id.")),
	), handleAbortTask)

	s.AddTool(mcp.NewTool(
		"batch_abort_tasks",
		mcp.WithDescription("Abort 1 to 100 Raw Tasks."),
		mcp.WithString("client_task_ids_json", mcp.Required(), mcp.Description("JSON array of client_task_id numbers.")),
	), handleBatchAbortTasks)

	s.AddTool(mcp.NewTool(
		"get_task_llm",
		mcp.WithDescription("Fetch the LLM result JSON text for a successful LLM Raw Task and return it in the tool result. Prefer this for short chat outputs; use the local MCP save_task_llm tool for large dumps."),
		mcp.WithNumber("client_task_id", mcp.Required(), mcp.Description("Local task_jobs.id of an LLM task.")),
	), handleGetTaskLLM)

	s.AddResource(mcp.NewResource(
		guideURIRawTask,
		"Raw Task and MCP guide",
		mcp.WithResourceDescription("Crynux AS MCP overview: workflow, model selection, TaskArgs pointers, Cost Level, and errors including model_not_ready."),
		mcp.WithMIMEType("text/markdown"),
	), handleGuideResource)

	s.AddResource(mcp.NewResource(
		guideURITaskArgsLLM,
		"LLM TaskArgs guide",
		mcp.WithResourceDescription("LLM TaskArgs fields, schema link, and common quantization examples."),
		mcp.WithMIMEType("text/markdown"),
	), handleGuideResource)

	s.AddResource(mcp.NewResource(
		guideURITaskArgsImage,
		"Image TaskArgs guide",
		mcp.WithResourceDescription("Image TaskArgs fields, schema link, LoRA, Bitsandbytes, and GGUF component examples."),
		mcp.WithMIMEType("text/markdown"),
	), handleGuideResource)

	s.AddResource(mcp.NewResource(
		guideURITaskArgsVideo,
		"Video TaskArgs guide",
		mcp.WithResourceDescription("Video TaskArgs fields, schema link, and T2V/I2V/FLF examples."),
		mcp.WithMIMEType("text/markdown"),
	), handleGuideResource)

	s.AddPrompt(mcp.NewPrompt(
		"run_llm_task",
		mcp.WithPromptDescription("Run one LLM Raw Task end-to-end and return the text result."),
		mcp.WithArgument("user_request",
			mcp.ArgumentDescription("What the user wants the LLM task to do."),
			mcp.RequiredArgument(),
		),
	), handleRunTaskPrompt)

	s.AddPrompt(mcp.NewPrompt(
		"run_image_task",
		mcp.WithPromptDescription("Run one Image Raw Task end-to-end and present public download URLs."),
		mcp.WithArgument("user_request",
			mcp.ArgumentDescription("What image the user wants to generate or edit."),
			mcp.RequiredArgument(),
		),
	), handleRunTaskPrompt)

	s.AddPrompt(mcp.NewPrompt(
		"run_video_task",
		mcp.WithPromptDescription("Run one Video Raw Task end-to-end and present the public download URL."),
		mcp.WithArgument("user_request",
			mcp.ArgumentDescription("What video the user wants to generate."),
			mcp.RequiredArgument(),
		),
	), handleRunTaskPrompt)

	return s
}

func handleListModels(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if _, err := getProject(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	catalog := strings.ToLower(strings.TrimSpace(req.GetString("catalog", "all")))
	var data []any
	switch catalog {
	case "", "all":
		data = service.ListModelsCatalogAll()
	case "chat":
		data = service.ListModelsCatalogChat()
	case "image":
		data = service.ListModelsCatalogImage()
	case "video":
		data = service.ListModelsCatalogVideo()
	default:
		return mcp.NewToolResultError("catalog must be all, chat, image, or video"), nil
	}
	return jsonToolResult(gin.H{"object": "list", "data": data})
}

func handleCreateTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	createReq, err := parseCreateRequest(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	outcome, err := raw_task.CreateForProject(ctx, project, createReq)
	if err != nil {
		return nil, err
	}
	if outcome.ErrorMessage != "" && outcome.View == nil {
		return mcp.NewToolResultError(outcome.ErrorMessage), nil
	}
	payload := gin.H{}
	if outcome.View != nil {
		payload["client_task"] = *outcome.View
	}
	if outcome.ModelNotReady {
		payload["error"] = outcome.ErrorMessage
		payload["client_task_id"] = outcome.ClientTaskID
		return jsonToolResult(payload)
	}
	return jsonToolResult(payload)
}

func handleBatchCreateTasks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw := strings.TrimSpace(req.GetString("tasks_json", ""))
	var tasks []raw_task.CreateTaskRequest
	if err := json.Unmarshal([]byte(raw), &tasks); err != nil {
		return mcp.NewToolResultError("tasks_json must be a JSON array of create task objects"), nil
	}
	results, hasError, err := raw_task.BatchCreateForProject(ctx, project, tasks)
	if err != nil {
		if errors.Is(err, raw_task.ErrInvalidBatchSize) {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return nil, err
	}
	return jsonToolResult(gin.H{"has_error": hasError, "data": results})
}

func handleGetTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := req.RequireInt("client_task_id")
	if err != nil || id <= 0 {
		return mcp.NewToolResultError("client_task_id is required"), nil
	}
	view, err := raw_task.GetForProject(ctx, project.ID, uint(id))
	if err != nil {
		if errors.Is(err, raw_task.ErrTaskNotFound) {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return nil, err
	}
	return jsonToolResult(view)
}

func handleBatchGetTaskStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ids, err := parseIDList(req.GetString("client_task_ids_json", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	items, err := raw_task.BatchStatusForProject(ctx, project.ID, ids)
	if err != nil {
		if errors.Is(err, raw_task.ErrInvalidBatchSize) {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return nil, err
	}
	return jsonToolResult(gin.H{"data": items})
}

func handleAbortTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := req.RequireInt("client_task_id")
	if err != nil || id <= 0 {
		return mcp.NewToolResultError("client_task_id is required"), nil
	}
	view, err := raw_task.AbortForProject(ctx, project.ID, uint(id))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonToolResult(view)
}

func handleBatchAbortTasks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ids, err := parseIDList(req.GetString("client_task_ids_json", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	items, err := raw_task.BatchAbortForProject(ctx, project.ID, ids)
	if err != nil {
		if errors.Is(err, raw_task.ErrInvalidBatchSize) {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return nil, err
	}
	return jsonToolResult(gin.H{"data": items})
}

func handleGetTaskLLM(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, err := getProject(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := req.RequireInt("client_task_id")
	if err != nil || id <= 0 {
		return mcp.NewToolResultError("client_task_id is required"), nil
	}
	body, err := raw_task.GetLLMResultForProject(ctx, project.ID, uint(id))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

func handleGuideResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	_ = ctx
	path, ok := guideFiles[req.Params.URI]
	if !ok {
		return nil, fmt.Errorf("unknown resource %s", req.Params.URI)
	}
	text, err := readGuideFile(path)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     text,
		},
	}, nil
}

func handleRunTaskPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	_ = ctx
	userRequest := ""
	if req.Params.Arguments != nil {
		userRequest = req.Params.Arguments["user_request"]
	}
	text, err := renderPromptTemplate(req.Params.Name, userRequest)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{
		"run_llm_task":   "Run one LLM Raw Task",
		"run_image_task": "Run one Image Raw Task",
		"run_video_task": "Run one Video Raw Task",
	}
	return mcp.NewGetPromptResult(
		titles[req.Params.Name],
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(text)),
		},
	), nil
}

func parseCreateRequest(req mcp.CallToolRequest) (raw_task.CreateTaskRequest, error) {
	taskArgs := strings.TrimSpace(req.GetString("task_args", ""))
	if taskArgs == "" {
		return raw_task.CreateTaskRequest{}, errors.New("task_args is required")
	}
	taskTypeNum, err := req.RequireFloat("task_type")
	if err != nil {
		return raw_task.CreateTaskRequest{}, errors.New("task_type is required")
	}
	taskType := models.TaskType(int(taskTypeNum))
	out := raw_task.CreateTaskRequest{
		TaskArgs: taskArgs,
		TaskType: &taskType,
	}
	if v := req.GetFloat("min_vram", 0); v > 0 {
		minVram := uint64(v)
		out.MinVram = &minVram
	}
	if v := req.GetFloat("queue_timeout", 0); v > 0 {
		timeout := uint64(v)
		out.QueueTimeout = &timeout
	}
	return out, nil
}

func parseIDList(raw string) ([]uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("client_task_ids_json is required")
	}
	var ids []uint
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, errors.New("client_task_ids_json must be a JSON array of numbers")
	}
	return ids, nil
}

func jsonToolResult(v any) (*mcp.CallToolResult, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(body)), nil
}
