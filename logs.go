package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type LogConfig struct {
	Dir        string // 日志目录
	FileName   string // 基础日志文件名(不含后缀)
	Level      string // debug info warn error dpanic panic fatal
	MaxAge     int    // 保存天数
	MaxSize    int    // 单个日志文件最大 MB
	MaxBackups int    // 备份数量
	LocalTime  bool   // Deprecated: 仅兼容 v1/v2.0.0，建议使用 UseLocalTime
	Console    bool   // Deprecated: 仅兼容 v1/v2.0.0，建议使用 EnableConsole

	UseLocalTime   *bool // nil:使用默认值(true), false:UTC, true:本地时间
	EnableConsole  *bool // nil:使用默认值(true), false:关闭控制台, true:开启控制台
	EnableFile     *bool // nil:使用默认值(true), false:关闭文件输出, true:开启文件输出
	SplitErrorFile *bool // nil:使用默认值(true), false:不拆分错误文件, true:拆分错误文件
	EnableAsync    *bool // nil:默认(false), true:启用异步文件缓冲写

	BufferSizeKB    int // 异步缓冲区大小(KB)
	FlushIntervalMs int // 异步刷盘间隔(ms)

	SamplingInitial    int // 采样窗口内前 N 条全量
	SamplingThereafter int // 超过后每 M 条保留 1 条
	SamplingTickMs     int // 采样窗口时长(ms)
}

var (
	errNilConfig     = errors.New("log config is nil")
	errEmptyFile     = errors.New("file name must not be empty")
	errInvalidDir    = errors.New("log dir must not be empty")
	errInvalidMaxAge = errors.New("max age must be greater than 0")
	errInvalidLevel  = errors.New("invalid level")
	errNoOutput      = errors.New("at least one output must be enabled")
	errInvalidBuffer = errors.New("buffer size must be greater than 0")
	errInvalidFlush  = errors.New("flush interval must be greater than 0")

	mu          sync.RWMutex
	initMu      sync.Mutex
	loggerRef   atomic.Pointer[zap.SugaredLogger]
	currentConf LogConfig
	logFileHook *lumberjack.Logger
	errFileHook *lumberjack.Logger
	logBuffered *zapcore.BufferedWriteSyncer
	errBuffered *zapcore.BufferedWriteSyncer
)

func DefaultConfig() LogConfig {
	return LogConfig{
		Dir:             "logs",
		FileName:        "log",
		Level:           "debug",
		MaxAge:          20,
		MaxSize:         100,
		MaxBackups:      10,
		LocalTime:       true,
		Console:         true,
		BufferSizeKB:    256,
		FlushIntervalMs: 1000,
		SamplingTickMs:  1000,
	}
}

func (c LogConfig) withDefaults() LogConfig {
	d := DefaultConfig()
	if c.Dir != "" {
		d.Dir = c.Dir
	}
	if c.FileName != "" {
		d.FileName = c.FileName
	}
	if c.Level != "" {
		d.Level = c.Level
	}
	if c.MaxAge > 0 {
		d.MaxAge = c.MaxAge
	}
	if c.MaxSize > 0 {
		d.MaxSize = c.MaxSize
	}
	if c.MaxBackups > 0 {
		d.MaxBackups = c.MaxBackups
	}
	if c.UseLocalTime != nil {
		d.LocalTime = *c.UseLocalTime
	} else if c.LocalTime {
		// 兼容旧代码显式设置 LocalTime=true 的场景。
		d.LocalTime = true
	}
	if c.EnableConsole != nil {
		d.Console = *c.EnableConsole
	} else if c.Console {
		// 兼容旧代码显式设置 Console=true 的场景。
		d.Console = true
	}
	if c.EnableFile != nil {
		d.EnableFile = boolPtr(*c.EnableFile)
	}
	if c.SplitErrorFile != nil {
		d.SplitErrorFile = boolPtr(*c.SplitErrorFile)
	}
	if c.EnableAsync != nil {
		d.EnableAsync = boolPtr(*c.EnableAsync)
	}
	if c.BufferSizeKB > 0 {
		d.BufferSizeKB = c.BufferSizeKB
	}
	if c.FlushIntervalMs > 0 {
		d.FlushIntervalMs = c.FlushIntervalMs
	}
	if c.SamplingInitial > 0 {
		d.SamplingInitial = c.SamplingInitial
	}
	if c.SamplingThereafter > 0 {
		d.SamplingThereafter = c.SamplingThereafter
	}
	if c.SamplingTickMs > 0 {
		d.SamplingTickMs = c.SamplingTickMs
	}

	return d
}

func (c LogConfig) validate() error {
	if strings.TrimSpace(c.Dir) == "" {
		return errInvalidDir
	}
	if strings.TrimSpace(c.FileName) == "" {
		return errEmptyFile
	}
	if c.MaxAge <= 0 {
		return errInvalidMaxAge
	}
	if c.BufferSizeKB <= 0 {
		return errInvalidBuffer
	}
	if c.FlushIntervalMs <= 0 || c.SamplingTickMs <= 0 {
		return errInvalidFlush
	}

	level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	if err := level.UnmarshalText([]byte(c.Level)); err != nil {
		return fmt.Errorf("%w: %s", errInvalidLevel, c.Level)
	}

	return nil
}

func Init(cfg LogConfig) error {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}

	level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return fmt.Errorf("%w: %s", errInvalidLevel, cfg.Level)
	}
	logLevel := level.Level()

	enableFile := true
	if cfg.EnableFile != nil {
		enableFile = *cfg.EnableFile
	}
	splitErrorFile := true
	if cfg.SplitErrorFile != nil {
		splitErrorFile = *cfg.SplitErrorFile
	}
	if !enableFile && !cfg.Console {
		return errNoOutput
	}
	enableAsync := false
	if cfg.EnableAsync != nil {
		enableAsync = *cfg.EnableAsync
	}

	cfg.EnableFile = boolPtr(enableFile)
	cfg.SplitErrorFile = boolPtr(splitErrorFile)
	cfg.EnableAsync = boolPtr(enableAsync)

	var logHook *lumberjack.Logger
	var errHook *lumberjack.Logger
	var logBuffer *zapcore.BufferedWriteSyncer
	var errBuffer *zapcore.BufferedWriteSyncer
	var logSink zapcore.WriteSyncer
	var errSink zapcore.WriteSyncer
	if enableFile {
		if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
			return err
		}
		logHook = &lumberjack.Logger{
			Filename:   filepath.Join(cfg.Dir, fmt.Sprintf("%s.log", cfg.FileName)),
			MaxAge:     cfg.MaxAge,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			LocalTime:  cfg.LocalTime,
		}
		if splitErrorFile {
			errHook = &lumberjack.Logger{
				Filename:   filepath.Join(cfg.Dir, fmt.Sprintf("%s_err.log", cfg.FileName)),
				MaxAge:     cfg.MaxAge,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				LocalTime:  cfg.LocalTime,
			}
		}

		logSink = zapcore.AddSync(logHook)
		if enableAsync {
			logBuffer = &zapcore.BufferedWriteSyncer{
				WS:            logSink,
				Size:          cfg.BufferSizeKB * 1024,
				FlushInterval: time.Duration(cfg.FlushIntervalMs) * time.Millisecond,
			}
			logSink = logBuffer
		}
		if splitErrorFile && errHook != nil {
			errSink = zapcore.AddSync(errHook)
			if enableAsync {
				errBuffer = &zapcore.BufferedWriteSyncer{
					WS:            errSink,
					Size:          cfg.BufferSizeKB * 1024,
					FlushInterval: time.Duration(cfg.FlushIntervalMs) * time.Millisecond,
				}
				errSink = errBuffer
			}
		}
	}

	consoleColoredEncoderConfig := zap.NewProductionEncoderConfig()
	consoleColoredEncoderConfig.TimeKey = "time"
	consoleColoredEncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleColoredEncoderConfig.EncodeTime = func(t time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}

	fileEncoderConfig := zap.NewProductionEncoderConfig()
	fileEncoderConfig.TimeKey = "time"
	fileEncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	fileEncoderConfig.EncodeTime = func(t time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}

	filePriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= logLevel
	})
	errPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= logLevel && lvl >= zapcore.ErrorLevel
	})
	stdoutPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= logLevel && lvl < zapcore.ErrorLevel
	})

	fileEncoder := zapcore.NewJSONEncoder(fileEncoderConfig)
	consoleEncoder := zapcore.NewConsoleEncoder(consoleColoredEncoderConfig)

	cores := make([]zapcore.Core, 0, 4)
	if enableFile {
		cores = append(cores, newCoreWithSampling(fileEncoder, logSink, filePriority, cfg))
		if splitErrorFile && errHook != nil {
			cores = append(cores, newCoreWithSampling(fileEncoder, errSink, errPriority, cfg))
		}
	}
	if cfg.Console {
		cores = append(cores,
			newCoreWithSampling(consoleEncoder, zapcore.Lock(os.Stdout), stdoutPriority, cfg),
			newCoreWithSampling(consoleEncoder, zapcore.Lock(os.Stderr), errPriority, cfg),
		)
	}

	logger := zap.New(zapcore.NewTee(cores...), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()

	mu.Lock()
	oldLogger := loggerRef.Load()
	oldLogFileHook := logFileHook
	oldErrFileHook := errFileHook
	oldLogBuffered := logBuffered
	oldErrBuffered := errBuffered

	loggerRef.Store(logger)
	currentConf = cfg
	logFileHook = logHook
	errFileHook = errHook
	logBuffered = logBuffer
	errBuffered = errBuffer
	mu.Unlock()

	if oldLogger != nil {
		_ = oldLogger.Sync()
	}
	if oldLogBuffered != nil {
		_ = oldLogBuffered.Stop()
	}
	if oldErrBuffered != nil {
		_ = oldErrBuffered.Stop()
	}
	if oldLogFileHook != nil {
		_ = oldLogFileHook.Close()
	}
	if oldErrFileHook != nil {
		_ = oldErrFileHook.Close()
	}

	return nil
}

// InitLogSetting 兼容旧版本 API。建议改用 Init。
func InitLogSetting(cfg *LogConfig) error {
	if cfg == nil {
		return errNilConfig
	}
	return Init(*cfg)
}

// GetLogConf 获取当前日志配置的副本。修改返回值不会影响全局配置。
func GetLogConf() *LogConfig {
	cfg := CurrentConfig()
	return &cfg
}

func CurrentConfig() LogConfig {
	mu.RLock()
	defer mu.RUnlock()

	if loggerRef.Load() == nil {
		return DefaultConfig()
	}
	return currentConf
}

func Close() error {
	mu.Lock()
	oldLogger := loggerRef.Load()
	oldLogFileHook := logFileHook
	oldErrFileHook := errFileHook
	oldLogBuffered := logBuffered
	oldErrBuffered := errBuffered
	loggerRef.Store(nil)
	logFileHook = nil
	errFileHook = nil
	logBuffered = nil
	errBuffered = nil
	currentConf = LogConfig{}
	mu.Unlock()

	if oldLogger != nil {
		_ = oldLogger.Sync()
	}
	if oldLogBuffered != nil {
		_ = oldLogBuffered.Stop()
	}
	if oldErrBuffered != nil {
		_ = oldErrBuffered.Stop()
	}
	if oldLogFileHook != nil {
		_ = oldLogFileHook.Close()
	}
	if oldErrFileHook != nil {
		_ = oldErrFileHook.Close()
	}
	return nil
}

// PrintPanicStack 在 defer 中调用，用于记录 panic 与堆栈后继续抛出 panic。
func PrintPanicStack(extras ...interface{}) {
	if x := recover(); x != nil {
		logger := getLogger()
		logger.Errorw("panic recovered", "panic", x, "stack", string(debug.Stack()))
		for i := range extras {
			logger.Errorf("EXTRAS#%d DATA:%+v", i, extras[i])
		}
		panic(x)
	}
}

func getLogger() *zap.SugaredLogger {
	logger := loggerRef.Load()
	if logger != nil {
		return logger
	}

	initMu.Lock()
	defer initMu.Unlock()
	logger = loggerRef.Load()
	if logger != nil {
		return logger
	}

	_ = Init(DefaultConfig())

	logger = loggerRef.Load()
	if logger != nil {
		return logger
	}

	return zap.NewNop().Sugar()
}

func Debug(v ...interface{}) {
	getLogger().Debug(v...)
}

func Debugf(format string, v ...interface{}) {
	getLogger().Debugf(format, v...)
}

func Debugw(msg string, keysAndValues ...interface{}) {
	getLogger().Debugw(msg, keysAndValues...)
}

func Info(v ...interface{}) {
	getLogger().Info(v...)
}

func Infof(format string, v ...interface{}) {
	getLogger().Infof(format, v...)
}

func Infow(msg string, keysAndValues ...interface{}) {
	getLogger().Infow(msg, keysAndValues...)
}

func Warn(v ...interface{}) {
	getLogger().Warn(v...)
}

func Warnf(format string, v ...interface{}) {
	getLogger().Warnf(format, v...)
}

func Warnw(msg string, keysAndValues ...interface{}) {
	getLogger().Warnw(msg, keysAndValues...)
}

func Error(v ...interface{}) {
	getLogger().Error(v...)
}

func Errorf(format string, v ...interface{}) {
	getLogger().Errorf(format, v...)
}

func Errorw(msg string, keysAndValues ...interface{}) {
	getLogger().Errorw(msg, keysAndValues...)
}

func Fatal(v ...interface{}) {
	getLogger().Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	getLogger().Fatalf(format, v...)
}

func Fatalw(msg string, keysAndValues ...interface{}) {
	getLogger().Fatalw(msg, keysAndValues...)
}

func Panic(v ...interface{}) {
	getLogger().Panic(v...)
}

func Panicf(format string, v ...interface{}) {
	getLogger().Panicf(format, v...)
}

func Panicw(msg string, keysAndValues ...interface{}) {
	getLogger().Panicw(msg, keysAndValues...)
}

func Sync() error {
	return getLogger().Sync()
}

func With(args ...interface{}) *zap.SugaredLogger {
	return getLogger().With(args...)
}

func boolPtr(v bool) *bool {
	return &v
}

func newCoreWithSampling(encoder zapcore.Encoder, ws zapcore.WriteSyncer, enabler zapcore.LevelEnabler, cfg LogConfig) zapcore.Core {
	core := zapcore.NewCore(encoder, ws, enabler)
	if cfg.SamplingInitial > 0 && cfg.SamplingThereafter > 0 {
		tick := time.Duration(cfg.SamplingTickMs) * time.Millisecond
		core = zapcore.NewSamplerWithOptions(core, tick, cfg.SamplingInitial, cfg.SamplingThereafter)
	}
	return core
}
