package logs

import (
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
	"runtime"
	"sync"
	"time"
)

type LogConfig struct {
	FileName  string // 日志文件名
	Level     string // 日志级别 debug info warn error dpanic panic fatal
	MaxAge    int    // 保存时间 单位天
	LocalTime bool   // true 使用本地时间  false 使用UTC时间
}

var (
	zapDefault, _  = zap.NewProduction()
	l              = zapDefault.Sugar()
	logFileHook    *lumberjack.Logger
	errLogFileHook *lumberjack.Logger
	once           sync.Once

	// default conf
	conf = &LogConfig{
		FileName:  "log",
		Level:     "debug",
		MaxAge:    20,
		LocalTime: true,
	}
)

func init() {
	once.Do(func() {
		InitLogSetting(conf)
	})
}

// GetLogConf 获取日志配置
func GetLogConf() *LogConfig {
	return conf
}

func InitLogSetting(conf *LogConfig) {
	// 初始化的日志级别
	level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	_ = level.UnmarshalText([]byte(conf.Level))

	logLevel := level.Level()
	// 保留20天, 分级别输出
	logFileHook = &lumberjack.Logger{
		Filename:  fmt.Sprintf("./logs/%s.log", conf.FileName),
		MaxAge:    conf.MaxAge,
		LocalTime: true,
	}
	errLogFileHook = &lumberjack.Logger{
		Filename:  fmt.Sprintf("./logs/%s_err.log", conf.FileName),
		MaxAge:    conf.MaxAge,
		LocalTime: true,
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
	consoleEncoder := zapcore.NewConsoleEncoder(consoleColoredEncoderConfig)
	fileEncoder := zapcore.NewConsoleEncoder(fileEncoderConfig)
	cores := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logFileHook), filePriority),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(errLogFileHook), errPriority),
		zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), stdoutPriority),
		zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stderr), errPriority),
	)
	// error级别输出调用栈信息
	logger := zap.New(cores, zap.AddStacktrace(zap.NewAtomicLevelAt(zap.ErrorLevel)))
	l = logger.Sugar()
}

// PrintPanicStack 产生panic时的调用栈打印
func PrintPanicStack(extras ...interface{}) {
	if x := recover(); x != nil {
		Error(x)
		i := 0
		funcName, file, line, ok := runtime.Caller(i)
		for ok {
			Errorf("frame %v:[func:%v,file:%v,line:%v]\n", i, runtime.FuncForPC(funcName).Name(), file, line)
			i++
			funcName, file, line, ok = runtime.Caller(i)
		}

		for k := range extras {
			Errorf("EXTRAS#%v DATA:%v\n", k, spew.Sdump(extras[k]))
		}
	}
}

func Debug(v ...interface{}) {
	l.Debug(v...)
}

func Debugf(format string, v ...interface{}) {
	l.Debugf(format, v...)
}

func Debugw(format string, keysAndValues ...interface{}) {
	l.Debugw(format, keysAndValues...)
}

func Info(v ...interface{}) {
	l.Info(v...)
}

func Infof(format string, v ...interface{}) {
	l.Infof(format, v...)
}

func Infow(format string, keysAndValues ...interface{}) {
	l.Infow(format, keysAndValues...)
}

func Warn(v ...interface{}) {
	l.Warn(v...)
}

func Warnf(format string, v ...interface{}) {
	l.Warnf(format, v...)
}

func Warnw(format string, keysAndValues ...interface{}) {
	l.Warnw(format, keysAndValues...)
}

func Error(v ...interface{}) {
	l.Error(v...)
}

func Errorf(format string, v ...interface{}) {
	l.Errorf(format, v...)
}

func Errorw(format string, keysAndValues ...interface{}) {
	l.Errorw(format, keysAndValues...)
}

func Fatal(v ...interface{}) {
	l.Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	l.Fatalf(format, v...)
}

func Fatalw(format string, keysAndValues ...interface{}) {
	l.Fatalw(format, keysAndValues...)
}

func Panic(v ...interface{}) {
	l.Panic(v...)
}

func Panicf(format string, v ...interface{}) {
	l.Panicf(format, v...)
}

func Panicw(format string, keysAndValues ...interface{}) {
	l.Panicw(format, keysAndValues...)
}

func Sync() error {
	return l.Sync()
}

func With(args ...interface{}) *zap.SugaredLogger {
	return l.With(args)
}
