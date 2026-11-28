// Package logger 构建项目统一的 zap logger，并定义**日志级别的语义**。
//
// # 四档级别（用户 2026-11-28 指定）
//
//	error  只抛出错误           —— 排障时用，正常运行时应当几乎为空
//	info   只打印必要信息       —— **默认**。启动/关闭、关键业务动作、异常请求
//	debug  打印调试信息         —— 加健康探针、内部流程分支、缓存命中与否
//	trace  打印全量信息         —— 加请求体/响应体、SQL 明细等（量大，仅短时开启）
//
// # 各级别具体该写什么（实现约定，改代码时按这个判）
//
//	ERROR  · 5xx、panic、外部依赖失败（DB/Redis/ES/上游 API）
//	       · 业务异常但需要人介入的（数据不一致、状态机异常）
//	       · **不写**：4xx（那是客户端的错）、可自愈的重试
//
//	INFO   · 启停、配置加载、worker 启动、迁移完成
//	       · 关键业务动作：下单、支付成功、审核通过/驳回、封禁
//	       · 访问日志：**正常请求（2xx/3xx）与 4xx**
//	       · **不写**：健康探针（见 DEBUG）、循环里的逐条处理
//
//	DEBUG  · 健康探针成功（Docker HEALTHCHECK 每 30s 一次，30s 一条会淹没 INFO）
//	       · 缓存命中/未命中、降级回退（ES→DB）、重试次数
//	       · 发件池状态流转、查询条件拼装结果
//
//	TRACE  · 完整请求/响应体、SQL 语句与参数、HTTP 上游原始报文
//	       · 只在本地复现问题时临时开启，**生产不要常开**
//
// # 为什么要这套约定
//
// 曾经的实现（2026-11-28 之前）有两个问题：
//  1. `LOG_LEVEL` 配置**根本不生效** —— main.go 硬编码 `zap.NewProduction()`，
//     永远是 InfoLevel。配了 `LOG_LEVEL=debug` 也没用。
//  2. 健康探针走 INFO：Docker HEALTHCHECK 每 30s 一次，一天 2880 条恒 200 的日志，
//     实测占生产日志量 99% 以上，把真正的业务日志淹没。
package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TraceLevel 比 zap 内置的 DebugLevel 更细一档（zap 的 Debug 是 -1）。
//
// zap 原生只有 Debug(-1)/Info(0)/Warn(1)/Error(2)，没有 trace。
// 用 -2 自定义，这样 `logger.Log(TraceLevel, ...)` 与 zapcore 的过滤链都能正常工作。
const TraceLevel = zapcore.Level(-2)

// stderr 日志输出目标（单独抽出来便于测试替换）。
var stderr = os.Stderr

// levelNames 是我们的四档 → zapcore.Level 的映射。
//
// 注意 `warn` 不对外暴露（用户只要求四档），但配置里写了 warn 也不报错 ——
// 归一到 info，避免升级时旧配置直接启动失败。
func parseLevel(s string) (zapcore.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return zapcore.ErrorLevel, true
	case "info", "", "warn", "warning":
		// 空值与 warn 都归到 info：空值是"没配"（用默认），warn 是历史写法。
		return zapcore.InfoLevel, true
	case "debug":
		return zapcore.DebugLevel, true
	case "trace":
		return TraceLevel, true
	}
	return zapcore.InfoLevel, false
}

// Options 构造参数。
type Options struct {
	// Level error / info / debug / trace（大小写不敏感）。非法值回落 info 并返回 warning。
	Level string
	// Format json / console。生产建议 json（便于采集），本地建议 console（人眼可读）。
	Format string
	// Development 开发模式：DPanic 会 panic、堆栈更详细。生产必须是 false。
	Development bool
}

// New 按配置构建 logger。
//
// 返回的第二个值是「级别解析失败的提示」（非法配置时非空），调用方应打一条 warn ——
// 因为那时 logger 还没建好，不能自己记自己。级别非法不阻断启动（回落 info）。
func New(opt Options) (*zap.Logger, string) {
	lv, ok := parseLevel(opt.Level)
	var warning string
	if !ok {
		warning = "LOG_LEVEL 取值非法（只支持 error/info/debug/trace），已回落 info: " + opt.Level
	}

	encCfg := zap.NewProductionEncoderConfig()
	if opt.Format == "console" {
		// 本地可读：时间用人眼格式、颜色分级
		encCfg = zap.NewDevelopmentEncoderConfig()
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	var encoder zapcore.Encoder
	if opt.Format == "console" {
		encoder = zapcore.NewConsoleEncoder(encCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encCfg)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.Lock(zapcore.AddSync(stderr)),
		zap.NewAtomicLevelAt(lv),
	)

	// 生产加 caller（定位到文件:行），开发加堆栈
	opts := []zap.Option{zap.AddCaller()}
	if opt.Development {
		opts = append(opts, zap.AddStacktrace(zapcore.DPanicLevel))
	} else {
		opts = append(opts, zap.AddStacktrace(zapcore.ErrorLevel))
	}

	return zap.New(core, opts...), warning
}

// LevelOf 把配置值解析成 zapcore.Level（供中间件判断「当前级别下要不要打这条」）。
func LevelOf(s string) zapcore.Level {
	lv, _ := parseLevel(s)
	return lv
}
