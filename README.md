# logs

`logs` 是一个基于 `zap + lumberjack` 的轻量日志库，支持：
- 控制台输出
- 普通日志与错误日志分文件
- 日志等级过滤
- 按文件大小/保留天数轮转

## Install

```bash
go get github.com/xpfo-go/logs/v2@latest
```

## Quick Start

```go
import "github.com/xpfo-go/logs/v2"

useLocalTime := true
enableConsole := true
enableFile := true
splitErrFile := true

cfg := logs.LogConfig{
    Dir:            "./logs",
    FileName:       "app",
    Level:          "info",
    MaxAge:         14,
    MaxSize:        100,
    MaxBackups:     10,
    UseLocalTime:   &useLocalTime,
    EnableConsole:  &enableConsole,
    EnableFile:     &enableFile,
    SplitErrorFile: &splitErrFile,
    // 可选并发优化
    // EnableAsync:     boolPtr(true),
    // BufferSizeKB:    512,
    // FlushIntervalMs: 100,
    // SamplingInitial: 100,
    // SamplingThereafter: 1000,
    // SamplingTickMs: 1000,
}

if err := logs.Init(cfg); err != nil {
    panic(err)
}
defer logs.Close()

logs.Info("service started")
logs.Errorw("request failed", "path", "/healthz", "status", 500)
```

## Config

```go
type LogConfig struct {
    Dir        string // 日志目录
    FileName   string // 基础日志名，不含后缀
    Level      string // debug/info/warn/error/dpanic/panic/fatal
    MaxAge     int    // 保留天数
    MaxSize    int    // 单个文件最大 MB
    MaxBackups int    // 备份文件数
    LocalTime  bool   // Deprecated，建议使用 UseLocalTime
    Console    bool   // Deprecated，建议使用 EnableConsole

    UseLocalTime   *bool // nil:默认(true), false:UTC
    EnableConsole  *bool // nil:默认(true), false:关闭控制台
    EnableFile     *bool // nil:默认(true), false:关闭文件输出
    SplitErrorFile *bool // nil:默认(true), false:不拆分错误文件
    EnableAsync    *bool // nil:默认(false), true:启用异步文件缓冲写

    BufferSizeKB    int // 异步缓冲区大小(KB)
    FlushIntervalMs int // 异步刷盘间隔(ms)

    SamplingInitial    int // 采样窗口内前 N 条全量
    SamplingThereafter int // 超过后每 M 条保留 1 条
    SamplingTickMs     int // 采样窗口时长(ms)
}
```

## High Concurrency Tips

高并发场景建议：
- 开启异步文件写：`EnableAsync=true`
- 适当增大缓冲区：`BufferSizeKB=256~1024`
- 缩短刷盘间隔：`FlushIntervalMs=50~200`
- 对高频重复日志开启采样：
  - `SamplingInitial=100`
  - `SamplingThereafter=1000`
  - `SamplingTickMs=1000`

基准测试：

```bash
go test -bench . -benchmem
```

## Output Files

初始化后会创建两个文件：
- `<Dir>/<FileName>.log`：`Level` 以上的所有日志
- `<Dir>/<FileName>_err.log`：仅错误级别日志（`error+`）

## Panic Handling

`PrintPanicStack` 用于 `defer` 场景，记录 panic 与堆栈后会重新抛出 panic：

```go
defer logs.PrintPanicStack("extra context")
```

## Compatibility

- 推荐 API：`Init(LogConfig) error`
- 兼容 API：`InitLogSetting(*LogConfig) error`（内部转调 `Init`）
- `GetLogConf()` 返回的是当前配置副本，修改返回值不会影响全局 logger

## Migration to v2

v2 主要变化：
- 模块路径改为 `github.com/xpfo-go/logs/v2`
- 配置初始化改为显式校验并返回错误（非法 level 会报错）
- 修复 `LocalTime` 配置不生效问题
- 增加输出控制：`EnableFile` / `EnableConsole` / `SplitErrorFile`
- 删除导入时自动初始化副作用，改为惰性默认初始化（首次写日志）或显式 `Init`
- `PrintPanicStack` 记录后会重新抛出 panic（不再吞 panic）
- 文件日志输出改为 JSON 编码（更适合日志采集系统）

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

## License

MIT. See [LICENSE](LICENSE).
