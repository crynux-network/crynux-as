package models

import (
	"strings"
	"testing"
)

func TestValidateTaskArgsQuantizationCompatAbsentPasses(t *testing.T) {
	taskArgs := `{"base_model":{"name":"org/image"},"prompt":"a","task_config":{"image_width":1024,"image_height":1024,"steps":30,"num_images_per_prompt":1}}`
	if err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeImage); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsIntegerThreshold(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"quant_kwargs":{"load_in_8bit":true,"llm_int8_threshold":6}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM)
	if err == nil || !strings.Contains(err.Error(), "llm_int8_threshold") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTaskArgsQuantizationCompatAcceptsFractionalThreshold(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"quant_kwargs":{"load_in_8bit":true,"llm_int8_threshold":6.0}
		}
	}`
	if err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsBothLoadFlags(t *testing.T) {
	taskArgs := `{
		"base_model":{"name":"org/image"},
		"prompt":"a",
		"task_config":{"image_width":1024,"image_height":1024,"steps":30,"num_images_per_prompt":1},
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"components_to_quantize":["transformer"],
			"quant_kwargs":{"load_in_4bit":true,"load_in_8bit":true}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeImage)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsUnknownKey(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"quant_kwargs":{"load_in_8bit":true,"not_a_real_field":true}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsCPUOffload(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"quant_kwargs":{"load_in_8bit":true,"llm_int8_enable_fp32_cpu_offload":true}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM)
	if err == nil || !strings.Contains(err.Error(), "llm_int8_enable_fp32_cpu_offload") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsImageNonBitsAndBytes(t *testing.T) {
	taskArgs := `{
		"base_model":{"name":"org/image"},
		"prompt":"a",
		"task_config":{"image_width":1024,"image_height":1024,"steps":30,"num_images_per_prompt":1},
		"quantization_config":{
			"quant_method":"fp8",
			"components_to_quantize":["transformer"],
			"quant_kwargs":{}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeImage)
	if err == nil || !strings.Contains(err.Error(), "bitsandbytes") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTaskArgsQuantizationCompatSkipsFP8FieldRules(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"fp8",
			"quant_kwargs":{"activation_scheme":"dynamic","dequantize":false}
		}
	}`
	if err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTaskArgsQuantizationCompatRejectsBadComputeDtype(t *testing.T) {
	taskArgs := `{
		"model":"org/llm",
		"messages":[{"role":"user","content":"hi"}],
		"quantization_config":{
			"quant_method":"bitsandbytes",
			"quant_kwargs":{"load_in_4bit":true,"bnb_4bit_compute_dtype":"float64"}
		}
	}`
	err := ValidateTaskArgsQuantizationCompat(taskArgs, TaskTypeLLM)
	if err == nil || !strings.Contains(err.Error(), "bnb_4bit_compute_dtype") {
		t.Fatalf("error = %v", err)
	}
}
