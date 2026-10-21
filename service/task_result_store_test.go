package service

import (
	"crynux_as/config"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveReadAndDeleteImageResultPNGs(t *testing.T) {
	root := t.TempDir()
	cfg := &config.AppConfig{}
	cfg.LLM.TaskResultDir = root
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	jobID := uint(42)
	if err := SaveImageResultPNGs(jobID, map[uint64][]byte{
		0: []byte("png-zero"),
		1: []byte("png-one"),
	}); err != nil {
		t.Fatal(err)
	}
	got0, err := ReadImageResultPNG(jobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got0) != "png-zero" {
		t.Fatalf("image 0 = %q", got0)
	}
	got1, err := ReadImageResultPNG(jobID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got1) != "png-one" {
		t.Fatalf("image 1 = %q", got1)
	}
	jobDir := filepath.Join(root, "42")
	if _, err := os.Stat(jobDir); err != nil {
		t.Fatalf("job dir missing: %v", err)
	}
	if err := DeleteTaskResultJobDir(jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
		t.Fatalf("job dir still present: %v", err)
	}
}

func TestImageCountFromTaskArgs(t *testing.T) {
	count, err := ImageCountFromTaskArgs(`{"task_config":{"num_images_per_prompt":3,"image_width":512,"image_height":512,"steps":20}}`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d", count)
	}
	count, err = ImageCountFromTaskArgs(`{"task_config":{"image_width":512,"image_height":512,"steps":20}}`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("default count = %d", count)
	}
}
