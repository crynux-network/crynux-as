package responses

import "testing"

func TestExtractModelFromBodyNormalizesModel(t *testing.T) {
	got := extractModelFromBody([]byte(`{"model":"  Qwen/Qwen2.5-7B  "}`))
	if got != "qwen/qwen2.5-7b" {
		t.Fatalf("extractModelFromBody=%q", got)
	}
	if got := extractModelFromBody([]byte(`{`)); got != "" {
		t.Fatalf("invalid body extractModelFromBody=%q", got)
	}
}
