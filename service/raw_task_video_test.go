package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"math/big"
	"testing"
)

const testVideoTaskArgs = `{"version":"3.8.0","base_model":{"name":"org/video"},"dtype":"bfloat16","prompt":"p","task_config":{"seed":1}}`

func TestExtractVideoTaskCallWorkload(t *testing.T) {
	fields := ExtractTaskCallWorkloadFields(models.TaskTypeVideo, "org/video", testVideoTaskArgs)
	if fields.DType != "bfloat16" || fields.ModelCacheHash == "" {
		t.Fatalf("fields = %+v", fields)
	}
	if fields.VideoWidth != 832 || fields.VideoHeight != 480 || fields.VideoNumFrames != 81 ||
		fields.VideoSteps != 30 || fields.VideoCount != 1 {
		t.Fatalf("video workload = %+v", fields)
	}
	sized := ExtractTaskCallWorkloadFields(models.TaskTypeVideo, "org/video",
		`{"base_model":{"name":"org/video"},"dtype":"float16","task_config":{"width":640,"height":360,"num_frames":49,"steps":12}}`)
	if sized.VideoWidth != 640 || sized.VideoHeight != 360 || sized.VideoNumFrames != 49 ||
		sized.VideoSteps != 12 || sized.VideoCount != 1 || sized.DType != "float16" {
		t.Fatalf("sized video workload = %+v", sized)
	}
	payload, err := decodeJSONObjectUseNumber([]byte(testVideoTaskArgs))
	if err != nil {
		t.Fatal(err)
	}
	_, wantHash, err := taskArgsModelCacheHash(modelCacheTaskKindVideo, "org/video", payload)
	if err != nil {
		t.Fatal(err)
	}
	if fields.ModelCacheHash != wantHash {
		t.Fatalf("hash = %q, want %q", fields.ModelCacheHash, wantHash)
	}
	imageHash := ExtractTaskCallWorkloadFields(models.TaskTypeImage, "org/video", testVideoTaskArgs).ModelCacheHash
	if imageHash == fields.ModelCacheHash {
		t.Fatal("video hash must differ from image hash for the same identity")
	}

	noDType := ExtractTaskCallWorkloadFields(models.TaskTypeVideo, "org/video", `{"base_model":{"name":"org/video"}}`)
	if noDType.DType != "" || noDType.ModelCacheHash != "" {
		t.Fatalf("fields without dtype = %+v", noDType)
	}
}

func TestTaskTypeAPIString(t *testing.T) {
	cases := map[models.TaskType]string{
		models.TaskTypeImage: "image",
		models.TaskTypeLLM:   "llm",
		models.TaskTypeVideo: "video",
	}
	for taskType, want := range cases {
		if got := TaskTypeAPIString(taskType); got != want {
			t.Fatalf("TaskTypeAPIString(%d) = %q, want %q", taskType, got, want)
		}
	}
}

func TestReserveVideoTaskRequiresSettlementCredits(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	project := models.Project{UserID: 1}
	_, err := ReserveCreditsAndCreateRawTaskJob(context.Background(), db, CreateRawTaskJobInput{
		Project: &project, TaskType: models.TaskTypeVideo, APIType: models.TaskAPITypeRaw,
		Model: "org/video", TaskArgsJSON: testVideoTaskArgs, PriorityGwei: big.NewInt(10),
		BilledVram: 8, TaskFeeWei: big.NewInt(0), CreditsToLock: big.NewInt(100),
	})
	if err == nil || err.Error() != "video settlement credits are required" {
		t.Fatalf("error = %v", err)
	}
}

func TestCompleteAndSettleVideoTaskChargesFixedCreditsOnce(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xvideosettle"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(500)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-video",
		APIKeyHash: "h-video", APIKeyPrefix: "prefixvid",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	job, err := ReserveCreditsAndCreateRawTaskJob(ctx, db, CreateRawTaskJobInput{
		Project: &project, TaskType: models.TaskTypeVideo, APIType: models.TaskAPITypeRaw,
		Model: "org/video", TaskArgsJSON: testVideoTaskArgs, PriorityGwei: big.NewInt(10),
		BilledVram: 8, TaskFeeWei: big.NewInt(0), SettlementCredits: big.NewInt(120),
		CreditsToLock: big.NewInt(120),
	})
	if err != nil {
		t.Fatal(err)
	}
	billing, err := job.DecodeBillingData()
	if err != nil {
		t.Fatal(err)
	}
	if billing.Video == nil || billing.Video.SettlementCredits != "120" || billing.Image != nil {
		t.Fatalf("billing = %+v", billing)
	}

	for i := 0; i < 2; i++ {
		if _, err := CompleteAndSettleFixedCreditsTaskJob(ctx, db, job); err != nil {
			t.Fatalf("settle %d: %v", i, err)
		}
	}
	if job.Status != models.TaskJobStatusCompleted || job.TaskCallRecordID == nil {
		t.Fatalf("job = %+v", job)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(380)) != 0 || account.Locked.Sign() != 0 {
		t.Fatalf("balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}
	var records []models.TaskCallRecord
	if err := db.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	record := records[0]
	if record.TaskType != models.TaskTypeVideo || record.DType != "bfloat16" || record.ModelCacheHash == "" {
		t.Fatalf("record = %+v", record)
	}
	if record.VideoWidth != 832 || record.VideoHeight != 480 || record.VideoNumFrames != 81 ||
		record.VideoSteps != 30 || record.VideoCount != 1 || record.ImageCount != 0 {
		t.Fatalf("record video workload = %+v", record)
	}
}
