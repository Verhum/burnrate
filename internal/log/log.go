package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	debug bool
}

func New(filePath string, debug bool) *Logger {
	var out io.Writer = os.Stderr
	if filePath != "" {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "burnrate: failed to open log file %s: %v\n", filePath, err)
		} else {
			out = io.MultiWriter(os.Stderr, f)
		}
	}
	return &Logger{out: out, debug: debug}
}

func (l *Logger) log(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s [%s] %s\n", ts, level, msg)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(l.out, line)
}

func (l *Logger) Infof(format string, args ...any)  { l.log("INFO", format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.log("WARN", format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.log("ERROR", format, args...) }
func (l *Logger) Debugf(format string, args ...any) {
	if l.debug {
		l.log("DEBUG", format, args...)
	}
}
