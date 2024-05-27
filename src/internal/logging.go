package internal

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
)

func getLogEncoder(usePlain bool, config zapcore.EncoderConfig) zapcore.Encoder {
	if usePlain {
		return zapcore.NewConsoleEncoder(config)
	}

	return zapcore.NewJSONEncoder(config)
}

func NewLogger(logLevel string, writePlainLogs bool) *zap.SugaredLogger {

	logLvl, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		panic(err)
	}

	infoLevel := zap.LevelEnablerFunc(func(level zapcore.Level) bool {
		return (level <= zapcore.WarnLevel) && (level >= logLvl)
	})

	errorLevel := zap.LevelEnablerFunc(func(level zapcore.Level) bool {
		return (level > zapcore.WarnLevel) && (level >= logLvl)
	})

	stdoutSyncer := zapcore.Lock(os.Stdout)
	stderrSyncer := zapcore.Lock(os.Stderr)

	core := zapcore.NewTee(
		zapcore.NewCore(
			getLogEncoder(writePlainLogs, zap.NewProductionEncoderConfig()),
			stdoutSyncer,
			infoLevel,
		),
		zapcore.NewCore(
			getLogEncoder(writePlainLogs, zap.NewProductionEncoderConfig()),
			stderrSyncer,
			errorLevel,
		),
	)

	logger := zap.New(core).Named("Root").Sugar()
	defer logger.Sync()

	return logger
}
