package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
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

	UseLocalTime  *bool // nil:使用默认值(true), false:UTC, true:本地时间
	EnableConsole *bool // nil:使用默认值(true), false:关闭控制台, true:开启控制台
}

var (
	errNilConfig     = errors.New("log config is nil")
	errEmptyFile     = errors.New("file name must not be empty")
	errInvalidDir    = errors.New("log dir must not be empty")
	errInvalidMaxAge = errors.New("max age must be greater than 0")
	errInvalidLevel  = errors.New("invalid level")

	mu           sync.RWMutex
	lazyInitOnce sync.Once
	l            *zap.SugaredLogger
	currentConf  LogConfig
	logFileHook  *lumberjack.Logger
	errFileHook  *lumberjack.Logger
)

func DefaultConfig() LogConfig {
	return LogConfig{
		Dir:        "logs",
		FileName:   "log",
		Level:      "debug",
		MaxAge:     20,
		MaxSize:    100,
		MaxBackups: 10,
		LocalTime:  true,
		Console:    true,
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

	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return err
	}

	level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return fmt.Errorf("%w: %s", errInvalidLevel, cfg.Level)
	}
	logLevel := level.Level()

	logHook := &lumberjack.Logger{
		Filename:   filepath.Join(cfg.Dir, fmt.Sprintf("%s.log", cfg.FileName)),
		MaxAge:     cfg.MaxAge,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		LocalTime:  cfg.LocalTime,
	}
	errHook := &lumberjack.Logger{
		Filename:   filepath.Join(cfg.Dir, fmt.Sprintf("%s_err.log", cfg.FileName)),
		MaxAge:     cfg.MaxAge,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		LocalTime:  cfg.LocalTime,
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

	cores := []zapcore.Core{
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logHook), filePriority),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(errHook), errPriority),
	}
	if cfg.Console {
		cores = append(cores,
			zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), stdoutPriority),
			zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stderr), errPriority),
		)
	}

	logger := zap.New(zapcore.NewTee(cores...), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()

	mu.Lock()
	oldLogger := l
	oldLogFileHook := logFileHook
	oldErrFileHook := errFileHook

	l = logger
	currentConf = cfg
	logFileHook = logHook
	errFileHook = errHook
	mu.Unlock()

	if oldLogger != nil {
		_ = oldLogger.Sync()
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

	if l == nil {
		return DefaultConfig()
	}
	return currentConf
}

func Close() error {
	mu.Lock()
	oldLogger := l
	oldLogFileHook := logFileHook
	oldErrFileHook := errFileHook
	l = nil
	logFileHook = nil
	errFileHook = nil
	currentConf = LogConfig{}
	mu.Unlock()

	if oldLogger != nil {
		_ = oldLogger.Sync()
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
	mu.RLock()
	logger := l
	mu.RUnlock()
	if logger != nil {
		return logger
	}

	lazyInitOnce.Do(func() {
		_ = Init(DefaultConfig())
	})

	mu.RLock()
	logger = l
	mu.RUnlock()
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
