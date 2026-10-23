package service

import (
	"crynux_as/models"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// taskSeedExclusiveMax is one past the largest seed accepted by Image TaskArgs.
const taskSeedExclusiveMax int64 = 2147483648

// EnsureTaskSeed writes a random generation seed when the client omitted one.
// An LLM seed is the top-level seed field. An Image seed is task_config.seed.
// A seed that is already present, including 0, is left unchanged.
func EnsureTaskSeed(taskArgsJSON string, taskType models.TaskType) (string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil || payload == nil {
		return "", errors.New("task_args must be a JSON object")
	}
	switch taskType {
	case models.TaskTypeLLM:
		if _, ok := payload["seed"]; ok {
			return taskArgsJSON, nil
		}
		encoded, err := randomTaskSeedJSON()
		if err != nil {
			return "", err
		}
		payload["seed"] = encoded
	case models.TaskTypeImage:
		updated, changed, err := imageTaskConfigWithSeed(payload["task_config"])
		if err != nil {
			return "", err
		}
		if !changed {
			return taskArgsJSON, nil
		}
		payload["task_config"] = updated
	default:
		return "", errors.New("task_type must be 0 or 1")
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func imageTaskConfigWithSeed(raw json.RawMessage) (json.RawMessage, bool, error) {
	cfg := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &cfg); err != nil || cfg == nil {
			return nil, false, errors.New("task_config must be a JSON object")
		}
		if _, ok := cfg["seed"]; ok {
			return nil, false, nil
		}
	}
	encoded, err := randomTaskSeedJSON()
	if err != nil {
		return nil, false, err
	}
	cfg["seed"] = encoded
	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func randomTaskSeedJSON() (json.RawMessage, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(taskSeedExclusiveMax))
	if err != nil {
		return nil, fmt.Errorf("generate task seed: %w", err)
	}
	encoded, err := json.Marshal(n.Int64())
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
