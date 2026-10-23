package models

import (
	"crynux_as/config"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/santhosh-tekuri/jsonschema/v5"
	_ "github.com/santhosh-tekuri/jsonschema/v5/httploader"
)

// MaxImageTaskArgsImagesBytes is the maximum total byte length of all Image
// TaskArgs `images` entries. Node workers reject WebSocket frames above 1 MiB,
// and those frames carry the full TaskArgs JSON dominated by these strings.
const MaxImageTaskArgsImagesBytes = 1 << 20

var sdInferenceTaskSchema *jsonschema.Schema
var gptInferenceTaskSchema *jsonschema.Schema
var sdFinetuneLoraTaskSchema *jsonschema.Schema

func ValidateTaskArgsJsonStr(jsonStr string, taskType TaskType) (validationError, err error) {
	if taskType == TaskTypeImage {
		return validateSDTaskArgs(jsonStr)
	} else if taskType == TaskTypeLLM {
		return validateGPTTaskArgs(jsonStr)
	}
	return validateSDFinetuneLoraTaskArgs(jsonStr)
}

// ValidateImageTaskArgsImagesSize rejects Image TaskArgs whose `images` strings
// exceed MaxImageTaskArgsImagesBytes in total. Length is measured on the JSON
// string values as stored (including data-URL prefixes); base64 is not decoded.
func ValidateImageTaskArgsImagesSize(jsonStr string) error {
	var payload struct {
		Images []string `json:"images"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return err
	}
	total := 0
	for _, image := range payload.Images {
		total += len(image)
		if total > MaxImageTaskArgsImagesBytes {
			return errors.New("task_args.images total size exceeds the 1mb limit")
		}
	}
	return nil
}

func validateSDTaskArgs(jsonStr string) (validationError, err error) {
	if sdInferenceTaskSchema == nil {
		schemaJSON := config.GetConfig().TaskSchema.StableDiffusionInference
		if !isValidURL(schemaJSON) {
			return nil, errors.New("invalid URL for task json schema")
		}
		sdInferenceTaskSchema, err = jsonschema.Compile(schemaJSON)
		if err != nil {
			return nil, err
		}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, err
	}
	return sdInferenceTaskSchema.Validate(v), nil
}

func validateGPTTaskArgs(jsonStr string) (validationError, err error) {
	if gptInferenceTaskSchema == nil {
		schemaJSON := config.GetConfig().TaskSchema.GPTInference
		if !isValidURL(schemaJSON) {
			return nil, errors.New("invalid URL for task json schema")
		}
		gptInferenceTaskSchema, err = jsonschema.Compile(schemaJSON)
		if err != nil {
			return nil, err
		}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, err
	}
	return gptInferenceTaskSchema.Validate(v), nil
}

func validateSDFinetuneLoraTaskArgs(jsonStr string) (validationError, err error) {
	if sdFinetuneLoraTaskSchema == nil {
		schemaJSON := config.GetConfig().TaskSchema.StableDiffusionFinetuneLora
		if !isValidURL(schemaJSON) {
			return nil, errors.New("invalid URL for task json schema")
		}
		sdFinetuneLoraTaskSchema, err = jsonschema.Compile(schemaJSON)
		if err != nil {
			return nil, err
		}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, err
	}
	return sdFinetuneLoraTaskSchema.Validate(v), nil
}

func isValidURL(toTest string) bool {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return false
	}
	u, err := url.Parse(toTest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return true
}

// ResetTaskArgsSchemasForTest clears compiled schemas so tests can reload config URLs.
func ResetTaskArgsSchemasForTest() {
	sdInferenceTaskSchema = nil
	gptInferenceTaskSchema = nil
	sdFinetuneLoraTaskSchema = nil
}
