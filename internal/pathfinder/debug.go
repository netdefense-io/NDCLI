package pathfinder

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/netdefense-io/NDCLI/internal/config"
)

var (
	debugLogger *log.Logger
	debugOnce   sync.Once
	debugFile   *os.File
)

// initDebugLogger initializes the debug logger based on config settings.
// It's safe to call multiple times - only the first call has effect.
func initDebugLogger() {
	debugOnce.Do(func() {
		cfg := config.Get()
		if !cfg.Debug.Enabled {
			return
		}

		logPath := cfg.Debug.LogFile
		if logPath == "" {
			// Default to config directory
			configDir := filepath.Dir(config.GetConfigFilePath())
			logPath = filepath.Join(configDir, "debug.log")
		}

		var err error
		debugFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			// Fall back to stderr if we can't open the log file
			fmt.Fprintf(os.Stderr, "Warning: could not open debug log file %s: %v\n", logPath, err)
			debugLogger = log.New(os.Stderr, "[DEBUG] ", log.LstdFlags)
			return
		}

		debugLogger = log.New(debugFile, "", log.LstdFlags|log.Lmicroseconds)
		debugLogger.Printf("Debug logging started")
	})
}

// debugLog writes a debug message if debug logging is enabled.
func debugLog(format string, args ...interface{}) {
	initDebugLogger()
	if debugLogger != nil {
		debugLogger.Printf(format, args...)
	}
}

// CloseDebugLogger closes the debug log file if it was opened.
// Should be called on application exit.
func CloseDebugLogger() {
	if debugFile != nil {
		debugFile.Close()
	}
}
