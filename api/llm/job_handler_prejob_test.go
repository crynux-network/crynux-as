package llm

import (
	"crynux_as/config"
	"crynux_as/models"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPreJobRejectDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:prejob_reject_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.CreditAccount{},
		&models.Project{},
		&models.TaskJob{},
		&models.TaskCallRecord{},
		&models.UsageStatsPendingCallRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	config.SetDBForTest(db)
	t.Cleanup(func() { config.SetDBForTest(nil) })
	return db
}

func TestChatCompletionsInvalidBodyWithoutCallRecord(t *testing.T) {
	db := setupPreJobRejectDB(t)
	cfg := &config.AppConfig{}
	cfg.LLM.DefaultMaxTokens = 512
	cfg.LLM.MaxCrynuxTaskArgsBytes = 1000000
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xchatreject1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "ep-chat-reject",
		APIKeyHash: "h", APIKeyPrefix: "pref",
		PriorityGwei: models.BigInt{Int: *big.NewInt(1)},
		Status:       models.ProjectStatusActive,
		CreditsDay:   models.BigInt{Int: *big.NewInt(0)},
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set(projectContextKey, &project)
		ChatCompletions(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var jobs, records, pending int64
	if err := db.Model(&models.TaskJob{}).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TaskCallRecord{}).Count(&records).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.UsageStatsPendingCallRecord{}).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || records != 0 || pending != 0 {
		t.Fatalf("jobs=%d records=%d pending=%d, want all 0", jobs, records, pending)
	}
}
