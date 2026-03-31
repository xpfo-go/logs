package logs

import (
	"path/filepath"
	"strconv"
	"testing"
)

func BenchmarkInfoSyncFile(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "sync")
	if err := Init(LogConfig{
		Dir:           dir,
		FileName:      "sync",
		Level:         "info",
		MaxAge:        1,
		EnableConsole: boolPtr(false),
		EnableFile:    boolPtr(true),
		EnableAsync:   boolPtr(false),
	}); err != nil {
		b.Fatalf("init failed: %v", err)
	}
	b.Cleanup(func() { _ = Close() })

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			Info("bench-sync", "i", i)
			i++
		}
	})
}

func BenchmarkInfoAsyncFile(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "async")
	if err := Init(LogConfig{
		Dir:             dir,
		FileName:        "async",
		Level:           "info",
		MaxAge:          1,
		EnableConsole:   boolPtr(false),
		EnableFile:      boolPtr(true),
		EnableAsync:     boolPtr(true),
		BufferSizeKB:    512,
		FlushIntervalMs: 50,
	}); err != nil {
		b.Fatalf("init failed: %v", err)
	}
	b.Cleanup(func() { _ = Close() })

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			Info("bench-async", "i", i)
			i++
		}
	})
}

func BenchmarkInfoAsyncSampling(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "sampling")
	if err := Init(LogConfig{
		Dir:                dir,
		FileName:           "sampling",
		Level:              "info",
		MaxAge:             1,
		EnableConsole:      boolPtr(false),
		EnableFile:         boolPtr(true),
		EnableAsync:        boolPtr(true),
		SplitErrorFile:     boolPtr(false),
		BufferSizeKB:       512,
		FlushIntervalMs:    50,
		SamplingInitial:    10,
		SamplingThereafter: 1000,
		SamplingTickMs:     1000,
	}); err != nil {
		b.Fatalf("init failed: %v", err)
	}
	b.Cleanup(func() { _ = Close() })

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			Info("bench-sampled-"+strconv.Itoa(i%5), "i", i)
			i++
		}
	})
}
