package llm

import (
	"crynux_as/api/tools"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/utils"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:auth_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Project{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	config.SetDBForTest(db)
	t.Cleanup(func() {
		config.SetDBForTest(nil)
	})
	return db
}

func setupAuthJWT(t *testing.T) {
	t.Helper()
	prev := tools.DefaultJWTManager
	tools.DefaultJWTManager = tools.NewJWTManager("test-jwt-secret", time.Hour)
	t.Cleanup(func() {
		tools.DefaultJWTManager = prev
	})
}

func seedOwnerProject(t *testing.T, db *gorm.DB, address, endpointToken, apiKey string, status models.ProjectStatus) (*models.User, *models.Project) {
	t.Helper()
	user := &models.User{Address: address}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &models.Project{
		UserID:        user.ID,
		Name:          "test",
		EndpointToken: endpointToken,
		APIKeyHash:    utils.HashToken(apiKey),
		APIKeyPrefix:  "test",
		CostLevelMode: models.CostLevelModeStatic,
		PriorityGwei:  models.BigInt{Int: *big.NewInt(1)},
		Status:        status,
		CreditsDay:    models.BigInt{Int: *big.NewInt(0)},
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	return user, project
}

func performAuthRequest(t *testing.T, endpointToken, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/:endpoint_token/v1/ping", ProjectAuthMiddleware(), func(c *gin.Context) {
		project := GetProject(c)
		if project == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing project"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "project_id": project.ID})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/"+endpointToken+"/v1/ping", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestProjectAuthAPIKeySuccess(t *testing.T) {
	db := setupAuthTestDB(t)
	setupAuthJWT(t)
	_, project := seedOwnerProject(t, db, "0x1111111111111111111111111111111111111111", "ep-api-key", "secret-api-key", models.ProjectStatusActive)

	w := performAuthRequest(t, project.EndpointToken, "secret-api-key")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProjectAuthJWTOwnerSuccess(t *testing.T) {
	db := setupAuthTestDB(t)
	setupAuthJWT(t)
	address := "0x2222222222222222222222222222222222222222"
	_, project := seedOwnerProject(t, db, address, "ep-jwt-owner", "other-key", models.ProjectStatusActive)

	token, _, err := tools.GenerateToken(address)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	w := performAuthRequest(t, project.EndpointToken, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProjectAuthJWTNonOwnerFails(t *testing.T) {
	db := setupAuthTestDB(t)
	setupAuthJWT(t)
	_, project := seedOwnerProject(t, db, "0x3333333333333333333333333333333333333333", "ep-jwt-nonowner", "key-a", models.ProjectStatusActive)
	other := &models.User{Address: "0x4444444444444444444444444444444444444444"}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}

	token, _, err := tools.GenerateToken(other.Address)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	w := performAuthRequest(t, project.EndpointToken, token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertAuthErrorBody(t, w)
}

func TestProjectAuthInvalidTokenFails(t *testing.T) {
	db := setupAuthTestDB(t)
	setupAuthJWT(t)
	_, project := seedOwnerProject(t, db, "0x5555555555555555555555555555555555555555", "ep-invalid", "real-key", models.ProjectStatusActive)

	w := performAuthRequest(t, project.EndpointToken, "not-a-valid-credential")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertAuthErrorBody(t, w)
}

func TestProjectAuthDisabledProjectFails(t *testing.T) {
	db := setupAuthTestDB(t)
	setupAuthJWT(t)
	address := "0x6666666666666666666666666666666666666666"
	_, project := seedOwnerProject(t, db, address, "ep-disabled", "disabled-key", models.ProjectStatusDisabled)

	token, _, err := tools.GenerateToken(address)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	w := performAuthRequest(t, project.EndpointToken, token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["message"] != "project is disabled" {
		t.Fatalf("message=%v", errObj["message"])
	}
}

func assertAuthErrorBody(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %s", w.Body.String())
	}
	if errObj["type"] != "invalid_request_error" {
		t.Fatalf("type=%v", errObj["type"])
	}
}
