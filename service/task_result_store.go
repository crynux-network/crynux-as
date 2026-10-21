package service

import (
	"crynux_as/config"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func TaskResultDir() (string, error) {
	cfg := config.GetConfig()
	if cfg == nil {
		return "", errors.New("config is not set")
	}
	dir := cfg.LLM.TaskResultDir
	if dir == "" {
		return "", errors.New("llm.task_result_dir is not set")
	}
	return dir, nil
}

func TaskResultJobDir(jobID uint) (string, error) {
	root, err := TaskResultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, strconv.FormatUint(uint64(jobID), 10)), nil
}

func TaskResultImagePath(jobID uint, index uint64) (string, error) {
	jobDir, err := TaskResultJobDir(jobID)
	if err != nil {
		return "", err
	}
	return filepath.Join(jobDir, fmt.Sprintf("%d.png", index)), nil
}

func ImageCountFromTaskArgs(taskArgsJSON string) (uint64, error) {
	var args imageTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		return 0, fmt.Errorf("parse image task args: %w", err)
	}
	numImages := uint64(1)
	if args.TaskConfig.NumImagesPerPrompt != nil {
		numImages = *args.TaskConfig.NumImagesPerPrompt
	}
	if numImages == 0 {
		return 0, errors.New("num_images_per_prompt must be positive")
	}
	return numImages, nil
}

// SaveImageResultPNGs writes PNG bodies into a temporary job directory and
// renames it to the final result path when every expected index is present.
func SaveImageResultPNGs(jobID uint, images map[uint64][]byte) error {
	if len(images) == 0 {
		return errors.New("images are required")
	}
	jobDir, err := TaskResultJobDir(jobID)
	if err != nil {
		return err
	}
	parent := filepath.Dir(jobDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(parent, fmt.Sprintf("%d.tmp.*", jobID))
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	for index, body := range images {
		if len(body) == 0 {
			return fmt.Errorf("image index %d is empty", index)
		}
		path := filepath.Join(tmpDir, fmt.Sprintf("%d.png", index))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(jobDir)
	if err := os.Rename(tmpDir, jobDir); err != nil {
		return err
	}
	return nil
}

func ReadImageResultPNG(jobID uint, index uint64) ([]byte, error) {
	path, err := TaskResultImagePath(jobID, index)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("image index %d is empty", index)
	}
	return body, nil
}

func ReadImageResultPNGs(jobID uint, count uint64) ([][]byte, error) {
	if count == 0 {
		return nil, errors.New("image count must be positive")
	}
	pngs := make([][]byte, 0, count)
	for index := uint64(0); index < count; index++ {
		body, err := ReadImageResultPNG(jobID, index)
		if err != nil {
			return nil, err
		}
		pngs = append(pngs, body)
	}
	return pngs, nil
}

func DeleteTaskResultJobDir(jobID uint) error {
	jobDir, err := TaskResultJobDir(jobID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(jobDir); err != nil {
		return err
	}
	return nil
}
