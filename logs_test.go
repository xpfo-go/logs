package logs

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInit_InvalidLevel(t *testing.T) {
	err := Init(LogConfig{
		Dir:           t.TempDir(),
		FileName:      "app",
		Level:         "invalid",
		MaxAge:        1,
		UseLocalTime:  boolPtr(true),
		EnableConsole: boolPtr(false),
	})
	if err == nil {
		t.Fatalf("expected init error for invalid level")
	}
	if !errors.Is(err, errInvalidLevel) {
		t.Fatalf("expected errInvalidLevel, got %v", err)
	}
}

func TestInit_RoutesLogsByLevel(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "app",
		Level:         "info",
		MaxAge:        1,
		UseLocalTime:  boolPtr(true),
		EnableConsole: boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	Debug("debug-message")
	Info("info-message")
	Error("error-message")
	_ = Sync()

	logFile := filepath.Join(dir, "app.log")
	errFile := filepath.Join(dir, "app_err.log")

	logData := waitAndReadFile(t, logFile)
	errData := waitAndReadFile(t, errFile)

	if strings.Contains(logData, "debug-message") {
		t.Fatalf("debug logs should not appear in info level file")
	}
	if !strings.Contains(logData, "info-message") {
		t.Fatalf("info logs should appear in main log file")
	}
	if !strings.Contains(logData, "\"level\":\"INFO\"") {
		t.Fatalf("main log should use JSON format, got: %s", logData)
	}
	if !strings.Contains(logData, "error-message") {
		t.Fatalf("error logs should appear in main log file")
	}
	if strings.Contains(errData, "info-message") {
		t.Fatalf("info logs should not appear in error log file")
	}
	if !strings.Contains(errData, "error-message") {
		t.Fatalf("error logs should appear in error log file")
	}
}

func TestGetLogConf_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "app",
		Level:         "warn",
		MaxAge:        7,
		UseLocalTime:  boolPtr(true),
		EnableConsole: boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	c1 := GetLogConf()
	c1.MaxAge = 30

	c2 := GetLogConf()
	if c2.MaxAge != 7 {
		t.Fatalf("GetLogConf should return copy, expected MaxAge=7 got %d", c2.MaxAge)
	}
}

func TestPrintPanicStack_Repanic(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "panic",
		Level:         "debug",
		MaxAge:        1,
		UseLocalTime:  boolPtr(true),
		EnableConsole: boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic to be rethrown")
		}
		if r != "boom" {
			t.Fatalf("expected panic value boom, got %v", r)
		}
	}()

	func() {
		defer PrintPanicStack(map[string]string{"k": "v"})
		panic("boom")
	}()
}

func TestInit_DefaultBoolFallbacks(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:      dir,
		FileName: "app",
		Level:    "info",
		MaxAge:   1,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	cfg := CurrentConfig()
	if !cfg.LocalTime {
		t.Fatalf("expected default LocalTime=true")
	}
	if !cfg.Console {
		t.Fatalf("expected default Console=true")
	}
}

func TestInit_ExplicitBoolOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "app",
		Level:         "info",
		MaxAge:        1,
		UseLocalTime:  boolPtr(false),
		EnableConsole: boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	cfg := CurrentConfig()
	if cfg.LocalTime {
		t.Fatalf("expected LocalTime=false")
	}
	if cfg.Console {
		t.Fatalf("expected Console=false")
	}
}

func TestInit_NoOutputConfigured(t *testing.T) {
	if err := Init(LogConfig{
		Dir:           t.TempDir(),
		FileName:      "app",
		Level:         "info",
		MaxAge:        1,
		EnableFile:    boolPtr(false),
		EnableConsole: boolPtr(false),
	}); !errors.Is(err, errNoOutput) {
		t.Fatalf("expected errNoOutput, got %v", err)
	}
}

func TestInit_ConsoleOnly_NoLogFilesCreated(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "app",
		Level:         "info",
		MaxAge:        1,
		EnableFile:    boolPtr(false),
		EnableConsole: boolPtr(true),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	Info("console-only")
	_ = Sync()

	if _, err := os.Stat(filepath.Join(dir, "app.log")); !os.IsNotExist(err) {
		t.Fatalf("expected no app.log when file output disabled")
	}
	if _, err := os.Stat(filepath.Join(dir, "app_err.log")); !os.IsNotExist(err) {
		t.Fatalf("expected no app_err.log when file output disabled")
	}
}

func TestInit_NoErrorSplit_NoErrFile(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:            dir,
		FileName:       "app",
		Level:          "info",
		MaxAge:         1,
		EnableFile:     boolPtr(true),
		SplitErrorFile: boolPtr(false),
		EnableConsole:  boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	Error("no-split-error")
	_ = Sync()

	logData := waitAndReadFile(t, filepath.Join(dir, "app.log"))
	if !strings.Contains(logData, "no-split-error") {
		t.Fatalf("error should still be written into main log when split disabled")
	}
	if _, err := os.Stat(filepath.Join(dir, "app_err.log")); !os.IsNotExist(err) {
		t.Fatalf("expected no app_err.log when split error file disabled")
	}
}

func TestGetLogger_ReInitAfterClose(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tempWd := t.TempDir()
	if err := os.Chdir(tempWd); err != nil {
		t.Fatalf("chdir temp failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "app",
		Level:         "info",
		MaxAge:        1,
		EnableConsole: boolPtr(false),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	Info("after-close-should-reinit")
	cfg := CurrentConfig()
	if cfg.FileName != "log" {
		t.Fatalf("expected lazy default re-init after close, got %+v", cfg)
	}
	if err := Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestInit_SamplingReducesRepeatedLogs(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:                dir,
		FileName:           "sampled",
		Level:              "info",
		MaxAge:             1,
		EnableConsole:      boolPtr(false),
		EnableFile:         boolPtr(true),
		SplitErrorFile:     boolPtr(false),
		SamplingInitial:    1,
		SamplingThereafter: 1000,
		SamplingTickMs:     1000,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	for i := 0; i < 100; i++ {
		Info("sampled-message")
	}
	_ = Sync()

	logData := waitAndReadFile(t, filepath.Join(dir, "sampled.log"))
	lines := countNonEmptyLines(logData)
	if lines >= 20 {
		t.Fatalf("expected sampling to reduce lines, got %d lines", lines)
	}
}

func TestInit_AsyncWriteEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:             dir,
		FileName:        "async",
		Level:           "info",
		MaxAge:          1,
		EnableConsole:   boolPtr(false),
		EnableFile:      boolPtr(true),
		EnableAsync:     boolPtr(true),
		BufferSizeKB:    64,
		FlushIntervalMs: 50,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	Info("async-message")
	_ = Sync()
	logData := waitAndReadFile(t, filepath.Join(dir, "async.log"))
	if !strings.Contains(logData, "async-message") {
		t.Fatalf("expected async log message in file")
	}
}

func TestConcurrentInitAndLogging(t *testing.T) {
	dir := t.TempDir()
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "concurrent",
		Level:         "debug",
		MaxAge:        1,
		EnableConsole: boolPtr(false),
		EnableFile:    boolPtr(true),
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = Close()
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Info("concurrent-log", "g", id, "n", j)
				if j%50 == 0 {
					_ = Init(LogConfig{
						Dir:           dir,
						FileName:      "concurrent_" + strconv.Itoa(id),
						Level:         "debug",
						MaxAge:        1,
						EnableConsole: boolPtr(false),
						EnableFile:    boolPtr(true),
					})
				}
			}
		}(i)
	}
	wg.Wait()
	_ = Sync()
}

func waitAndReadFile(t *testing.T, path string) string {
	t.Helper()

	var data []byte
	var err error
	for i := 0; i < 50; i++ {
		data, err = os.ReadFile(path)
		if err == nil {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("failed to read file %s: %v", path, err)
	return ""
}

func countNonEmptyLines(s string) int {
	lines := strings.Split(s, "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
