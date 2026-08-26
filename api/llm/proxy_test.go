package llm

import (
	"bytes"
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupProxyTest(t *testing.T) (*gorm.DB, *models.Project) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:" + t.Name() + "?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.CreditAccount{},
		&models.Project{},
		&models.LLMCallRecord{},
		&models.CreditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{Address: "0xabc"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	account := models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1_000_000)},
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	project := models.Project{
		UserID:        user.ID,
		Name:          "test",
		EndpointToken: "endpoint",
		APIKeyHash:    "hash",
		APIKeyPrefix:  "prefix",
		TokenRatio:    10,
		Status:        models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	cfg := &config.AppConfig{}
	cfg.Environment = config.EnvTest
	cfg.LLM.PromptCreditsPerToken = 1
	cfg.LLM.CompletionCreditsPerToken = 1
	cfg.LLM.DefaultMaxTokens = 100
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	cfg.LLM.QueuedPriorityRefreshInterval = 300
	cfg.LLM.ExecutionTimeCacheTTL = 300
	cfg.LLM.BaseVRAM = 8
	cfg.LLM.EmptyQueueMedianPriorityGwei = 1
	cfg.LLM.VramRatios = []config.VramRatioConfig{{MaxVram: 96, Ratio: 1.0}}
	config.SetConfigForTest(cfg)
	config.SetDBForTest(db)

	t.Cleanup(func() {
		estimateTaskFeeFn = service.EstimateTaskFee
		config.SetConfigForTest(nil)
		config.SetDBForTest(nil)
	})

	return db, &project
}

func TestHandleLLMProxyRecordsTaskFeeOnSuccess(t *testing.T) {
	db, project := setupProxyTest(t)

	estimateTaskFeeFn = func(ctx context.Context, model string, effectiveVram uint64, tokenRatioStored uint, estimatedPromptTokens, maxCompletionTokens uint64) (*service.CalcTaskFeeResult, error) {
		estimated := 12.5
		weight := 2.0
		return &service.CalcTaskFeeResult{
			TaskFeeGwei:          big.NewInt(321),
			MedianPriorityGwei:   big.NewInt(11),
			EstimatedNodeSeconds: estimated,
			VramWeight:           weight,
		}, nil
	}

	forwardCalled := false
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"qwen/qwen3","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(projectContextKey, project)

	handleLLMProxy(
		c,
		func([]byte) uint64 { return 4 },
		func(ctx context.Context, vramLimit uint64, body []byte) (*bridge.Response, error) {
			forwardCalled = true
			respBody := []byte(`{"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)
			return &bridge.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		},
	)

	if !forwardCalled {
		t.Fatal("expected bridge forward")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var record models.LLMCallRecord
	if err := db.First(&record).Error; err != nil {
		t.Fatalf("load record: %v", err)
	}
	if record.Status != models.LLMCallStatusSuccess {
		t.Fatalf("status = %v, want success", record.Status)
	}
	if record.TaskFeeGwei == nil || record.TaskFeeGwei.Cmp(big.NewInt(321)) != 0 {
		t.Fatalf("TaskFeeGwei = %v, want 321", record.TaskFeeGwei)
	}
	if record.MedianPriorityGwei == nil || record.MedianPriorityGwei.Cmp(big.NewInt(11)) != 0 {
		t.Fatalf("MedianPriorityGwei = %v, want 11", record.MedianPriorityGwei)
	}
	if record.EstimatedNodeSeconds == nil || *record.EstimatedNodeSeconds != 12.5 {
		t.Fatalf("EstimatedNodeSeconds = %v, want 12.5", record.EstimatedNodeSeconds)
	}
	if record.VramWeight == nil || *record.VramWeight != 2.0 {
		t.Fatalf("VramWeight = %v, want 2.0", record.VramWeight)
	}
}

func TestHandleLLMProxyFeeFailureDoesNotForward(t *testing.T) {
	db, project := setupProxyTest(t)

	estimateTaskFeeFn = func(ctx context.Context, model string, effectiveVram uint64, tokenRatioStored uint, estimatedPromptTokens, maxCompletionTokens uint64) (*service.CalcTaskFeeResult, error) {
		return nil, errors.New("execution-time unavailable")
	}

	forwardCalled := false
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"qwen/qwen3","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(projectContextKey, project)

	handleLLMProxy(
		c,
		func([]byte) uint64 { return 4 },
		func(ctx context.Context, vramLimit uint64, body []byte) (*bridge.Response, error) {
			forwardCalled = true
			return nil, nil
		},
	)

	if forwardCalled {
		t.Fatal("bridge must not be forwarded when fee estimation fails")
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	var record models.LLMCallRecord
	if err := db.First(&record).Error; err != nil {
		t.Fatalf("load record: %v", err)
	}
	if record.Status != models.LLMCallStatusFailed {
		t.Fatalf("status = %v, want failed", record.Status)
	}
	if record.TaskFeeGwei != nil || record.MedianPriorityGwei != nil || record.EstimatedNodeSeconds != nil || record.VramWeight != nil {
		t.Fatalf("fee fields must be empty on fee estimation failure, got %+v", record)
	}
}

func TestHandleLLMProxyCreditsPathUnchangedWhenFeeSucceeds(t *testing.T) {
	db, project := setupProxyTest(t)

	estimateTaskFeeFn = func(ctx context.Context, model string, effectiveVram uint64, tokenRatioStored uint, estimatedPromptTokens, maxCompletionTokens uint64) (*service.CalcTaskFeeResult, error) {
		return &service.CalcTaskFeeResult{
			TaskFeeGwei:          big.NewInt(1),
			MedianPriorityGwei:   big.NewInt(1),
			EstimatedNodeSeconds: 1,
			VramWeight:           1,
		}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"qwen/qwen3","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(projectContextKey, project)

	handleLLMProxy(
		c,
		func([]byte) uint64 { return 10 },
		func(ctx context.Context, vramLimit uint64, body []byte) (*bridge.Response, error) {
			respBody := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			return &bridge.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var account models.CreditAccount
	if err := db.Where("user_id = ?", project.UserID).First(&account).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	// credits = (10*10*10*1 + 5*10*10*1) / 100 = 15
	if account.Balance.Cmp(big.NewInt(1_000_000-15)) != 0 {
		t.Fatalf("balance = %s, want %d", account.Balance.String(), 1_000_000-15)
	}

	var event models.CreditEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load credit event: %v", err)
	}
	if event.Type != models.CreditEventTypeLLMCharge {
		t.Fatalf("event type = %v", event.Type)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response json: %v", err)
	}
	_ = time.Now()
}
