// Package logging provides structured JSON logging for ADC using zap.
package logging

import (
	"os"

	"github.com/wso2/adc/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger wraps zap.Logger with ADC-specific methods.
type Logger struct {
	*zap.SugaredLogger
	base *zap.Logger
}

// New creates a new Logger based on server configuration.
func New(cfg config.ServerConfig) *Logger {
	level := parseLevel(cfg.LogLevel)
	encoder := newEncoder(cfg.LogFormat)
	writer := newWriter(cfg)

	core := zapcore.NewCore(encoder, writer, level)
	base := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{
		SugaredLogger: base.Sugar(),
		base:          base,
	}
}

// WithFields returns a child logger with additional structured fields.
func (l *Logger) WithFields(keysAndValues ...interface{}) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.With(keysAndValues...),
		base:          l.base,
	}
}

// Sync flushes buffered log entries.
func (l *Logger) Sync() {
	_ = l.base.Sync()
}

func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func newEncoder(format string) zapcore.Encoder {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "timestamp"
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.LowercaseLevelEncoder

	if format == "text" {
		return zapcore.NewConsoleEncoder(cfg)
	}
	return zapcore.NewJSONEncoder(cfg)
}

func newWriter(cfg config.ServerConfig) zapcore.WriteSyncer {
	if cfg.LogOutput == "" || cfg.LogOutput == "stdout" {
		return zapcore.AddSync(os.Stdout)
	}

	lj := &lumberjack.Logger{
		Filename:   cfg.LogOutput,
		MaxSize:    cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAge:     cfg.LogMaxAgeDays,
		Compress:   true,
	}

	return zapcore.NewMultiWriteSyncer(
		zapcore.AddSync(os.Stdout),
		zapcore.AddSync(lj),
	)
}
