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

const (
	projectContextKey contextKey = "mcp_project"
	originContextKey  contextKey = "mcp_public_origin"
)

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

func publicOrigin(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func projectFromGin(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if project := llm.GetProject(c); project != nil {
		ctx = context.WithValue(ctx, projectContextKey, project)
	}
	origin := publicOrigin(c.Request)
	ctx = context.WithValue(ctx, originContextKey, origin)
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

func getOrigin(ctx context.Context) string {
	origin, _ := ctx.Value(originContextKey).(string)
	return origin
}

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
		mcp.WithDescription("List Crynux AS models. catalog may be all, chat, image, or video."),
		mcp.WithString("catalog", mcp.Description("Optional catalog filter: all, chat, image, or video. Default all.")),
	), handleListModels)

	s.AddTool(mcp.NewTool(
		"create_task",
		mcp.WithDescription("Create one Raw Task. task_type is 0 Image, 1 LLM, or 3 Video. For Image/Video success, build public download URLs from result_download_hash. Remote MCP has no image/video save tools; install the local MCP package for bulk local save."),
		mcp.WithString("task_args", mcp.Required(), mcp.Description("TaskArgs JSON string for the selected task_type.")),
		mcp.WithNumber("task_type", mcp.Required(), mcp.Description("0 Image, 1 LLM, or 3 Video.")),
		mcp.WithNumber("min_vram", mcp.Description("Optional minimum VRAM in GB.")),
		mcp.WithNumber("queue_timeout", mcp.Description("Optional queue timeout in seconds.")),
	), handleCreateTask)

	s.AddTool(mcp.NewTool(
		"batch_create_tasks",
		mcp.WithDescription("Create 1 to 100 Raw Tasks in one call. Same task fields as create_task per item."),
		mcp.WithString("tasks_json", mcp.Required(), mcp.Description("JSON array of create task objects with task_args, task_type, and optional min_vram and queue_timeout.")),
	), handleBatchCreateTasks)

	s.AddTool(mcp.NewTool(
		"get_task",
		mcp.WithDescription("Get one Raw Task status view by client_task_id."),
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
		mcp.WithDescription("Fetch the LLM result JSON text for a successful LLM Raw Task and return it in the tool result."),
		mcp.WithNumber("client_task_id", mcp.Required(), mcp.Description("Local task_jobs.id of an LLM task.")),
	), handleGetTaskLLM)

	s.AddResource(mcp.NewResource(
		"crynux://guides/raw-task",
		"Raw Task and MCP guide",
		mcp.WithResourceDescription("How to create LLM, Image, and Video Raw Tasks over MCP, read LLM results, and use public Image/Video download links."),
		mcp.WithMIMEType("text/markdown"),
	), handleGuideResource)

	s.AddPrompt(mcp.NewPrompt(
		"cost_speed_quality",
		mcp.WithPromptDescription("Guidance for balancing Cost Level, speed, and result quality on Crynux AS."),
	), handleCostSpeedQualityPrompt)

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
		payload["client_task"] = enrichTaskView(*outcome.View, getOrigin(ctx))
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
	origin := getOrigin(ctx)
	out := make([]gin.H, 0, len(results))
	for _, item := range results {
		row := gin.H{"index": item.Index}
		if item.Error != "" {
			row["error"] = item.Error
		}
		if item.ClientTaskID != nil {
			row["client_task_id"] = *item.ClientTaskID
		}
		if item.ClientTask != nil {
			row["client_task"] = enrichTaskView(*item.ClientTask, origin)
		}
		out = append(out, row)
	}
	return jsonToolResult(gin.H{"has_error": hasError, "data": out})
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
	return jsonToolResult(enrichTaskView(*view, getOrigin(ctx)))
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
	origin := getOrigin(ctx)
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		row := gin.H{"client_task_id": item.ClientTaskID}
		if item.Error != "" {
			row["error"] = item.Error
		}
		if item.ClientTask != nil {
			row["client_task"] = enrichTaskView(*item.ClientTask, origin)
		}
		out = append(out, row)
	}
	return jsonToolResult(gin.H{"data": out})
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
	return jsonToolResult(enrichTaskView(*view, getOrigin(ctx)))
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
	origin := getOrigin(ctx)
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		row := gin.H{"client_task_id": item.ClientTaskID}
		if item.Error != "" {
			row["error"] = item.Error
		}
		if item.ClientTask != nil {
			row["client_task"] = enrichTaskView(*item.ClientTask, origin)
		}
		out = append(out, row)
	}
	return jsonToolResult(gin.H{"data": out})
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
	origin := getOrigin(ctx)
	text := `# Crynux AS Raw Task MCP

Use create_task / batch_create_tasks with task_type 0 (Image), 1 (LLM), or 3 (Video).

For LLM results, call get_task_llm when status is success.

For Image and Video results, Remote MCP does not download or save files.
Use result_download_hash from the task view:

- Image: ` + "`" + origin + `/api/result/{result_download_hash}/images/{index}` + "`" + `
- Video: ` + "`" + origin + `/api/result/{result_download_hash}/video` + "`" + `

For bulk local save of Image, Video, or large LLM text batches, install the local Crynux AS MCP package (stdio / mcpb) and use save_task_llm, save_task_image, and save_task_video.
`
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     text,
		},
	}, nil
}

func handleCostSpeedQualityPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	_ = ctx
	_ = req
	return mcp.NewGetPromptResult(
		"Cost, speed, and quality on Crynux AS",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleAssistant, mcp.NewTextContent(
				"Higher project Cost Level raises priority_gwei and usually shortens queue wait, at higher Credits cost. "+
					"Lower Cost Level saves Credits and may wait longer. "+
					"Model choice, dtype, resolution, steps, frames, and token limits dominate quality and runtime. "+
					"Use list_models to see ready models, then create_task with concrete TaskArgs. "+
					"Prefer get_task_llm for short LLM outputs in chat. Prefer the local MCP save tools for bulk Image/Video or large text dumps.",
			)),
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

func enrichTaskView(view raw_task.TaskView, origin string) gin.H {
	out := gin.H{
		"id":         view.ID,
		"task_type":  view.TaskType,
		"status":     view.Status,
		"phase":      view.Phase,
		"created_at": view.CreatedAt,
		"updated_at": view.UpdatedAt,
	}
	if view.EstimatedCompletionAt != nil {
		out["estimated_completion_at"] = view.EstimatedCompletionAt
	}
	if view.ErrorMessage != "" {
		out["error_message"] = view.ErrorMessage
	}
	if view.ResultDownloadHash != "" {
		out["result_download_hash"] = view.ResultDownloadHash
		if origin != "" {
			switch view.TaskType {
			case models.TaskTypeImage:
				out["result_download_url_template"] = fmt.Sprintf(
					"%s/api/result/%s/images/{index}", origin, view.ResultDownloadHash,
				)
				out["result_download_url_image_0"] = fmt.Sprintf(
					"%s/api/result/%s/images/0", origin, view.ResultDownloadHash,
				)
			case models.TaskTypeVideo:
				out["result_download_url"] = fmt.Sprintf(
					"%s/api/result/%s/video", origin, view.ResultDownloadHash,
				)
			}
		}
		out["local_save_hint"] = "Remote MCP cannot save Image/Video files. Install the local Crynux AS MCP package for save_task_image / save_task_video."
	}
	return out
}

func jsonToolResult(v any) (*mcp.CallToolResult, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(body)), nil
}
