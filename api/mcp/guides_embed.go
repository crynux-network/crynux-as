package mcp

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed guides
var guideFS embed.FS

const (
	guideURIRawTask       = "crynux://guides/raw-task"
	guideURICostLevel     = "crynux://guides/cost-level"
	guideURIVramModels    = "crynux://guides/vram-models"
	guideURITaskArgsLLM   = "crynux://guides/task-args/llm"
	guideURITaskArgsImage = "crynux://guides/task-args/image"
	guideURITaskArgsVideo = "crynux://guides/task-args/video"
)

var guideFiles = map[string]string{
	guideURIRawTask:       "guides/raw-task.md",
	guideURICostLevel:     "guides/cost-level.md",
	guideURIVramModels:    "guides/vram-models.md",
	guideURITaskArgsLLM:   "guides/task-args-llm.md",
	guideURITaskArgsImage: "guides/task-args-image.md",
	guideURITaskArgsVideo: "guides/task-args-video.md",
}

var promptFiles = map[string]string{
	"run_llm_task":   "guides/prompts/run_llm_task.md",
	"run_image_task": "guides/prompts/run_image_task.md",
	"run_video_task": "guides/prompts/run_video_task.md",
}

func readGuideFile(path string) (string, error) {
	body, err := guideFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read guide %s: %w", path, err)
	}
	return string(body), nil
}

func renderPromptTemplate(name, userRequest string) (string, error) {
	path, ok := promptFiles[name]
	if !ok {
		return "", fmt.Errorf("unknown prompt %s", name)
	}
	body, err := readGuideFile(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userRequest) == "" {
		userRequest = "(no user_request provided)"
	}
	return strings.ReplaceAll(body, "{{user_request}}", userRequest), nil
}
