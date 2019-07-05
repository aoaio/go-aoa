package log

import (
	"testing"
	"go.uber.org/zap/zapcore"
)

func TestZapLog(t *testing.T) {

	Debugf("hello world %s","xiaohua")
	Info("hello world")
	Warn("hello world")
	Debug(zapcore.DebugLevel)
	Debugf("this is a number %v", 12)
}
