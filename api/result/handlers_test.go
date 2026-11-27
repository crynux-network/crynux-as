package result

import (
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPublicDownloadByHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:result_hash_test?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TaskJob{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	config.SetDBForTest(db)
	t.Cleanup(func() { config.SetDBForTest(nil) })

	tmp := t.TempDir()
	cfg := &config.AppConfig{}
	cfg.LLM.TaskResultDir = tmp
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	hash := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	job := models.TaskJob{
		ProjectID:          1,
		UserID:             1,
		PriorityGwei:       models.BigInt{Int: *big.NewInt(1)},
		TaskType:           models.TaskTypeImage,
		Model:              "org/model",
		TaskArgsJSON:       `{}`,
		Status:             models.TaskJobStatusCompleted,
		BillingData:        `{"version":1,"priority_gwei":"1","billed_vram":24,"task_fee_wei":"0","image":{"settlement_credits":"1"}}`,
		ResultDownloadHash: &hash,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := service.SaveImageResultPNGs(job.ID, map[uint64][]byte{0: []byte("png-bytes")}); err != nil {
		t.Fatalf("save image: %v", err)
	}

	router := gin.New()
	InitRoutes(router)

	okReq := httptest.NewRequest(http.MethodGet, "/api/result/"+hash+"/images/0", nil)
	okRec := httptest.NewRecorder()
	router.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", okRec.Code)
	}
	if got := okRec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type: %s", got)
	}
	if okRec.Body.String() != "png-bytes" {
		t.Fatalf("body: %q", okRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodGet, "/api/result/unknownhash/images/0", nil)
	badRec := httptest.NewRecorder()
	router.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown hash, got %d", badRec.Code)
	}

	videoReq := httptest.NewRequest(http.MethodGet, "/api/result/"+hash+"/video", nil)
	videoRec := httptest.NewRecorder()
	router.ServeHTTP(videoRec, videoReq)
	if videoRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for image hash on video path, got %d", videoRec.Code)
	}
}
