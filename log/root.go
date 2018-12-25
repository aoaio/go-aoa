package log

import (
	"os"
)

var (
	root          = &logger{[]interface{}{}, new(swapHandler)}
	StdoutHandler = StreamHandler(os.Stdout, LogfmtFormat())
	StderrHandler = StreamHandler(os.Stderr, LogfmtFormat())
)

func init() {
	root.SetHandler(DiscardHandler())
}

func New(ctx ...interface{}) Logger {
	return root.New(ctx...)
}

func Root() Logger {
	return root
}

func Trace(msg string, ctx ...interface{}) {
	root.write(msg, LvlTrace, ctx)
}

func Debug(msg string, ctx ...interface{}) {
	root.write(msg, LvlDebug, ctx)
}

func Info(msg string, ctx ...interface{}) {
	root.write(msg, LvlInfo, ctx)
}

func Warn(msg string, ctx ...interface{}) {
	root.write(msg, LvlWarn, ctx)
}

func Error(msg string, ctx ...interface{}) {
	root.write(msg, LvlError, ctx)
}

func Crit(msg string, ctx ...interface{}) {
	root.write(msg, LvlCrit, ctx)
	os.Exit(1)
}
