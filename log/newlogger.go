package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"time"
)

type Field = zapcore.Field

// error logger
var errorLogger *zap.SugaredLogger
var logger *zap.Logger

var LogLevel = zap.InfoLevel
var atom = zap.NewAtomicLevel()

var levelMap = map[string]zapcore.Level{
	"debug":  zapcore.DebugLevel,
	"info":   zapcore.InfoLevel,
	"warn":   zapcore.WarnLevel,
	"error":  zapcore.ErrorLevel,
	"dpanic": zapcore.DPanicLevel,
	"panic":  zapcore.PanicLevel,
	"fatal":  zapcore.FatalLevel,
}

func getLoggerLevel(lvl string) zapcore.Level {
	if level, ok := levelMap[lvl]; ok {
		return level
	}
	return zapcore.InfoLevel
}

func init() {
	//fileName := "zap.log"
	//syncWriter := zapcore.AddSync(&lumberjack.Logger{
	//	Filename:  fileName,
	//	MaxSize:   1 << 30, //1G
	//	LocalTime: true,
	//	Compress:  true,
	//})
	//syncWriter2 := zapcore.AddSync(&zapcore.WriteSyncer())

	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}
	config.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncodeCaller = zapcore.ShortCallerEncoder

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(config),
		os.Stdout,
		atom,
		//zap.NewAtomicLevelAt(level)
	)
	atom.SetLevel(LogLevel)
	logger = zap.New(core, zap.Development(), zap.AddCallerSkip(1))
	errorLogger = logger.Sugar()
}

func SetLevel(level string) {
	LogLevel = getLoggerLevel(level)
	atom.SetLevel(LogLevel)
}

func Debug(args ...interface{}) {
	errorLogger.Debug(args...)
}

func Debugf(template string, args ...interface{}) {
	errorLogger.Debugf(template, args...)
}

func Info(args ...interface{}) {
	errorLogger.Info(args...)
}

func LInfo(msg string, fields ...Field) {
	logger.Info(msg, fields...)
}

func Infof(template string, args ...interface{}) {
	errorLogger.Infof(template, args...)
}

func Warn(args ...interface{}) {
	errorLogger.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	errorLogger.Warnf(template, args...)
}

func Error(args ...interface{}) {
	errorLogger.Error(args...)
}

func Errorf(template string, args ...interface{}) {
	errorLogger.Errorf(template, args...)
}

func DPanic(args ...interface{}) {
	errorLogger.DPanic(args...)
}

func DPanicf(template string, args ...interface{}) {
	errorLogger.DPanicf(template, args...)
}

func Panic(args ...interface{}) {
	errorLogger.Panic(args...)
}

func Panicf(template string, args ...interface{}) {
	errorLogger.Panicf(template, args...)
}

func Fatal(args ...interface{}) {
	errorLogger.Fatal(args...)
}

func Fatalf(template string, args ...interface{}) {
	errorLogger.Fatalf(template, args...)
}
