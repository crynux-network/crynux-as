package config

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestNewDBLoggerDebugFollowsLogLevel(t *testing.T) {
	prev := appConfig
	t.Cleanup(func() { appConfig = prev })

	appConfig = &AppConfig{}
	appConfig.Db.Log.Level = "info"
	appConfig.Db.Log.Output = "stderr"
	logger := NewDBLogger()
	if logger.Debug {
		t.Fatal("Debug was true when db.log.level is info")
	}

	appConfig.Db.Log.Level = "debug"
	logger = NewDBLogger()
	if !logger.Debug {
		t.Fatal("Debug was false when db.log.level is debug")
	}
}

func TestDBLoggerTraceOmitsSQLBindValues(t *testing.T) {
	var buf bytes.Buffer
	logrusInstance = log.New()
	logrusInstance.SetOutput(&buf)
	logrusInstance.SetLevel(log.DebugLevel)
	logrusInstance.SetFormatter(&log.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})

	logger := &Logger{
		SkipErrRecordNotFound: true,
		Debug:                 true,
		SlowThreshold:         time.Nanosecond,
	}
	secret := "super-secret-ciphertext-value-for-log-test"
	fc := func() (string, int64) {
		return "INSERT INTO shared_api_keys (key_ciphertext) VALUES ('" + secret + "')", 1
	}

	logger.Trace(context.Background(), time.Now().Add(-time.Millisecond), fc, errors.New("duplicate key"))
	logger.Trace(context.Background(), time.Now().Add(-time.Millisecond), fc, nil)
	logger.SlowThreshold = 0
	logger.Trace(context.Background(), time.Now(), fc, nil)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("db logger output contained SQL bind value: %s", out)
	}
	if !strings.Contains(out, "msg=\"db query failed\"") {
		t.Fatalf("error path missing: %s", out)
	}
	if !strings.Contains(out, "msg=\"db query slow\"") {
		t.Fatalf("slow path missing: %s", out)
	}
	if !strings.Contains(out, "msg=\"db query\"") {
		t.Fatalf("debug path missing: %s", out)
	}
}
