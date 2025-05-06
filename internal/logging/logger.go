package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

type Logger struct {
	component string
}

var (
	baseLogger *log.Logger
)

func InitLogger() error {
	logFile, err := os.OpenFile("logs/server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	multi := io.MultiWriter(os.Stdout, logFile)
	baseLogger = log.New(multi, "", 0) // we format manually, so no log.Flags

	return nil
}

func New(component string) *Logger {
	return &Logger{component: component}
}

func (l *Logger) Info(msg string, keyvals ...any) {
	l.output("INFO", msg, keyvals...)
}

func (l *Logger) Error(msg string, keyvals ...any) {
	l.output("ERROR", msg, keyvals...)
}

func (l *Logger) Warn(msg string, keyvals ...any) {
	l.output("WARN", msg, keyvals...)
}

func (l *Logger) output(level string, msg string, keyvals ...any) {
	timestamp := time.Now().UnixMilli()

	logLine := fmt.Sprintf("ts=%d level=%s component=%s msg=%q", timestamp, level, l.component, msg)

	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			logLine += fmt.Sprintf(" %v=%v", keyvals[i], keyvals[i+1])
		}
	}

	baseLogger.Println(logLine)
}
