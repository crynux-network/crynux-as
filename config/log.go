package config

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InitLog(appConfig *AppConfig) error {

	println("Initializing logger...")

	logrus.SetFormatter(&logrus.TextFormatter{})

	switch appConfig.Log.Output {
	case "", "stderr":
		logrus.SetOutput(os.Stderr)
	case "stdout":
		logrus.SetOutput(os.Stdout)
	default:
		logWriter := newLogWriter(appConfig.Log.Output, appConfig.Log.MaxFileSize, appConfig.Log.MaxDays, appConfig.Log.MaxFileNum)
		mw := io.MultiWriter(os.Stdout, logWriter)
		logrus.SetOutput(mw)
	}

	level, err := logrus.ParseLevel(appConfig.Log.Level)

	if err != nil {
		return err
	}

	logrus.SetLevel(level)

	return nil
}

func newLogWriter(filename string, maxFileSize, maxDays, maxFileNum int) *lumberjack.Logger {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		logrus.WithError(err).Warn("failed to create log directory")
	}
	logWriter := &lumberjack.Logger{
		Filename: filename,
		Compress: true,
	}

	if maxFileSize == 0 {
		logWriter.MaxSize = 500
	} else {
		logWriter.MaxSize = maxFileSize
	}

	if maxDays == 0 {
		logWriter.MaxAge = 30
	} else {
		logWriter.MaxAge = maxDays
	}

	if maxFileNum == 0 {
		logWriter.MaxBackups = 10
	} else {
		logWriter.MaxBackups = maxFileNum
	}

	return logWriter
}
