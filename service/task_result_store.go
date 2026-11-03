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
	return TaskResultImagePathWithExt(jobID, index, "png")
}

func TaskResultImagePathWithExt(jobID uint, index uint64, ext string) (string, error) {
	jobDir, err := TaskResultJobDir(jobID)
	if err != nil {
		return "", err
	}
	if ext == "" {
		ext = "png"
	}
	if ext == "jpeg" {
		ext = "jpg"
	}
	return filepath.Join(jobDir, fmt.Sprintf("%d.%s", index, ext)), nil
}

func ImageCountFromTaskArgs(taskArgsJSON string) (uint64, error) {
	var args imageTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err == nil {
		numImages := uint64(1)
		if args.TaskConfig.NumImagesPerPrompt != nil {
			numImages = *args.TaskConfig.NumImagesPerPrompt
		}
		if numImages == 0 {
			return 0, errors.New("num_images_per_prompt must be positive")
		}
		return numImages, nil
	}
	var providerBody struct {
		N *uint64 `json:"n"`
	}
	if err := json.Unmarshal([]byte(taskArgsJSON), &providerBody); err != nil {
		return 0, fmt.Errorf("parse image task args: %w", err)
	}
	if providerBody.N == nil {
		return 1, nil
	}
	if *providerBody.N == 0 {
		return 0, errors.New("n must be positive")
	}
	return *providerBody.N, nil
}

// SaveImageResultPNGs writes PNG bodies into a temporary job directory and
// renames it to the final result path when every expected index is present.
func SaveImageResultPNGs(jobID uint, images map[uint64][]byte) error {
	typed := make(map[uint64]ImageResultFile, len(images))
	for index, body := range images {
		typed[index] = ImageResultFile{Format: "png", Data: body}
	}
	return SaveImageResultFiles(jobID, typed)
}

// ImageResultFile is one encoded image stored under the job result directory.
type ImageResultFile struct {
	Format string
	Data   []byte
}

// SaveImageResultFiles writes encoded image bodies with format-specific
// extensions into the job result directory.
func SaveImageResultFiles(jobID uint, images map[uint64]ImageResultFile) error {
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

	for index, file := range images {
		if len(file.Data) == 0 {
			return fmt.Errorf("image index %d is empty", index)
		}
		ext := file.Format
		if ext == "" {
			ext = "png"
		}
		if ext == "jpeg" {
			ext = "jpg"
		}
		path := filepath.Join(tmpDir, fmt.Sprintf("%d.%s", index, ext))
		if err := os.WriteFile(path, file.Data, 0o644); err != nil {
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
	body, _, err := ReadImageResultFile(jobID, index)
	return body, err
}

func ReadImageResultFile(jobID uint, index uint64) ([]byte, string, error) {
	for _, ext := range []string{"png", "jpg", "webp"} {
		path, err := TaskResultImagePathWithExt(jobID, index, ext)
		if err != nil {
			return nil, "", err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", err
		}
		if len(body) == 0 {
			return nil, "", fmt.Errorf("image index %d is empty", index)
		}
		format := ext
		if format == "jpg" {
			format = "jpeg"
		}
		return body, format, nil
	}
	return nil, "", fmt.Errorf("image index %d not found", index)
}

func ReadImageResultPNGs(jobID uint, count uint64) ([][]byte, error) {
	files, err := ReadImageResultFiles(jobID, count)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	return out, nil
}

func ReadImageResultFiles(jobID uint, count uint64) ([][]byte, error) {
	if count == 0 {
		return nil, errors.New("image count must be positive")
	}
	out := make([][]byte, 0, count)
	for index := uint64(0); index < count; index++ {
		body, _, err := ReadImageResultFile(jobID, index)
		if err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, nil
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
