package server

import (
	"fmt"
	"io"
	"os"

	"cosmossdk.io/log"
	rlog "github.com/ipfs/go-log/v2"
)

var _ rlog.EventLogger = &logAdapter{}

type logAdapter struct {
	logger log.Logger
}

// NewLogAdapter constructor
func NewLogAdapter(logger log.Logger) rlog.EventLogger {
	if v, ok := logger.Impl().(rlog.EventLogger); ok {
		return v
	}
	return &logAdapter{logger: logger}
}

func (l logAdapter) Debug(args ...interface{}) {
	doLog(l.logger.Debug, args...)
}

func (l logAdapter) Debugf(format string, args ...interface{}) {
	doLogf(l.logger.Debug, format, args...)
}

func (l logAdapter) Info(args ...interface{}) {
	doLog(l.logger.Info, args...)
}

func (l logAdapter) Error(args ...interface{}) {
	doLog(l.logger.Error, args...)
}

func (l logAdapter) Errorf(format string, args ...interface{}) {
	doLogf(l.logger.Error, format, args...)
}

func (l logAdapter) Infof(format string, args ...interface{}) {
	doLogf(l.logger.Info, format, args...)
}

func (l logAdapter) Warn(args ...interface{}) {
	doLog(l.logger.Warn, args...)
}

func (l logAdapter) Warnf(format string, args ...interface{}) {
	doLogf(l.logger.Warn, format, args...)
}

func (l logAdapter) Panic(args ...interface{}) {
	doLog(l.logger.Error, args...)
	l.tryFlushAndClose()
	panic(getMessage("", args))
}

func (l logAdapter) Panicf(format string, args ...interface{}) {
	doLogf(l.logger.Error, format, args...)
	l.tryFlushAndClose()
	panic(getMessage(format, args))
}

func (l logAdapter) Fatal(args ...interface{}) {
	doLog(l.logger.Error, args...)
	l.tryFlushAndClose()
	os.Exit(1)
}

func (l logAdapter) Fatalf(format string, args ...interface{}) {
	doLogf(l.logger.Error, format, args...)
	l.tryFlushAndClose()
	os.Exit(1)
}

func (l logAdapter) tryFlushAndClose() {
	impl := l.logger.Impl()
	if flusher, ok := impl.(interface{ Sync() error }); ok {
		_ = flusher.Sync()
	}
	if closer, ok := impl.(io.Closer); ok {
		_ = closer.Close()
	}
}

func doLogf(fn func(msg string, keyVals ...any), format string, args ...any) {
	fn(getMessage(format, args))
}

func doLog(fn func(msg string, keyVals ...any), args ...any) {
	if len(args) == 0 {
		return
	}

	msg := "no-message"
	var kvs []any

	if str, ok := args[0].(string); ok {
		msg = str
		kvs = args[1:]
	} else {
		kvs = args
	}

	fn(msg, kvs...)
}

func getMessage(template string, fmtArgs []interface{}) string {
	if len(fmtArgs) == 0 {
		return template
	}

	if template != "" {
		return fmt.Sprintf(template, fmtArgs...)
	}

	if len(fmtArgs) == 1 {
		if str, ok := fmtArgs[0].(string); ok {
			return str
		}
	}
	return fmt.Sprint(fmtArgs...)
}
