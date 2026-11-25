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

func TestSaveReadAndDeleteVideoResultMP4(t *testing.T) {
	root := t.TempDir()
	cfg := &config.AppConfig{}
	cfg.LLM.TaskResultDir = root
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	jobID := uint(43)
	if err := SaveVideoResultMP4(jobID, nil); err == nil {
		t.Fatal("expected empty video error")
	}
	if err := SaveVideoResultMP4(jobID, []byte("mp4-first")); err != nil {
		t.Fatal(err)
	}
	if err := SaveVideoResultMP4(jobID, []byte("mp4-second")); err != nil {
		t.Fatal(err)
	}
	path, err := TaskResultVideoPath(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "43", "0.mp4") {
		t.Fatalf("path = %q", path)
	}
	got, err := ReadVideoResultMP4(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mp4-second" {
		t.Fatalf("video = %q", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "43" {
		t.Fatalf("result root entries = %v", entries)
	}
	if err := DeleteTaskResultJobDir(jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVideoResultMP4(jobID); !os.IsNotExist(err) {
		t.Fatalf("read after delete err = %v", err)
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
	count, err = ImageCountFromTaskArgs(`{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":3,"size":"1024x1024"}`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("provider n count = %d", count)
	}
}
