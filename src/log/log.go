package log

import (
	stdlog "log"
	"path/filepath"
	"sync"
	"time"

	charmlog "charm.land/log/v2"
	"gopkg.in/natefinch/lumberjack.v2"
)

var once sync.Once

// Setup configures application logging to write to logDir/jenktui.log
// with lumberjack-based rotation.
func Setup(logDir string) {
	once.Do(func() {
		w := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "jenktui.log"),
			MaxSize:    10, // MB
			MaxAge:     30, // days
			MaxBackups: 0,
			Compress:   false,
		}

		charmlog.SetOutput(w)
		charmlog.SetTimeFormat(time.Kitchen)
		charmlog.SetReportCaller(true)
		charmlog.SetLevel(charmlog.DebugLevel)
		charmlog.Debug("Logging initialized", "path", w.Filename)
	})
}

// GoJenkinsLogger returns a stdlib *log.Logger that writes to logDir/jenkins.log
// with lumberjack rotation. Suitable for gojenkins.SetLogger().
func GoJenkinsLogger(logDir string) *stdlog.Logger {
	w := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "jenkins.log"),
		MaxSize:    10,
		MaxAge:     30,
		MaxBackups: 0,
		Compress:   false,
	}
	return stdlog.New(w, "GOJENKINS: ", stdlog.Ldate|stdlog.Ltime)
}
