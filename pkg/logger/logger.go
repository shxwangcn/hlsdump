package logger

import (
	"errors"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger *zap.Logger
)

func Init(file string, level string) error {
	if logger != nil {
		return errors.New("logger already inited")
	}

	if file == "" {
		logger, _ = zap.NewProduction()
	} else {
		wcore := zapcore.AddSync(&lumberjack.Logger{
			Filename:   file,
			MaxSize:    500,  // megabytes
			MaxAge:     31,   // days
			MaxBackups: 31,   // the maximum number of old log files to retain
			Compress:   true, // use gzip to compress all rotated log files
		})
		zaplevel := zap.NewAtomicLevel()
		zaplevel.UnmarshalText([]byte(level))
		zapencCfg := zap.NewProductionEncoderConfig()
		zapencCfg.EncodeTime = zapcore.ISO8601TimeEncoder

		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(zapencCfg),
			wcore,
			zaplevel.Level(),
		)

		// append "caller", "pid" to log
		opts := []zap.Option{}
		opts = append(opts, zap.AddCaller())
		initFields := []zapcore.Field{
			zap.Any("pid", os.Getpid()),
		}
		opts = append(opts, zap.Fields(initFields...))
		logger = zap.New(core, opts...)
	}

	return nil
}

func Inst() *zap.Logger {
	return logger
}
