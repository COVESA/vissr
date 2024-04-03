/**
* (C) 2019 Mitsubishi Electrics Automotive
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"

	//	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
)

var (
	//    Trace   *log.Logger
	Info    *logrus.Logger
	Warning *logrus.Logger
	Error   *logrus.Logger
)

/*
* Possible inparams to Init:
* ioutil.Discard,
* os.Stdout,
* os.Stderr)
* utils.LogFile
 */

// const LOG_FILE = "servercore-log.txt"
var Logfile *os.File

func InitLog(filename string, logdir string, logFile bool, logLevel string) {

	logger := logrus.New()
	logger.SetReportCaller(true)
	logger.Formatter = &logrus.JSONFormatter{
		//DisableTimestamp: true,
		//TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			//s := strings.Split(f.Function, ".")
			//funcName := s[len(s)-1]
			_, fileName := path.Split(f.File)
			//return funcName, fmt.Sprintf("%s:%d", fileName, f.Line)
			return "", fmt.Sprintf("%s:%d", fileName, f.Line)
		},
		//PrettyPrint: true,
	}
	switch logLevel {
	case "trace":
		logger.SetLevel(logrus.TraceLevel)
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "info":
		logger.SetLevel(logrus.InfoLevel)
	case "warn":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	case "fatal":
		logger.SetLevel(logrus.FatalLevel)
	case "panic":
		logger.SetLevel(logrus.PanicLevel)
	}

	if logFile {
		os.MkdirAll(logdir, 0755)
		path := filepath.Join(logdir, filename)
		Logfile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
		if err != nil {
			logger.Fatal(err)
		}

		logger.SetOutput(Logfile)
		logrus.RegisterExitHandler(CloseLogFile)
	} else {
		iow := io.Writer(os.Stdout)
		logger.SetOutput(iow)
	}
	Info, Warning, Error = logger, logger, logger
}

func CloseLogFile() {
	if Logfile != nil {
		Logfile.Close()
	}

}

/**
* The log file is trimmed to 20% of its size when exceeding 10MB.
**/
func TrimLogFile(logFile *os.File) {
	fi, err2 := logFile.Stat()
	if err2 != nil {
		log.Fatalln("Failed to obtain log file stat", os.Stdout, ":", err2)
	}
	if fi.Size() > 10000000 { // 10 MB
		fout, err3 := os.Create("logtmp.txt")
		if err3 != nil {
			log.Fatalln("Failed to remove untrimmed log file", os.Stdout, ":", err3)
		}
		defer fout.Close()

		_, err4 := logFile.Seek(8000000, io.SeekStart) // trim 8MB
		if err4 != nil {
			log.Fatalln("Failed to open log file", os.Stdout, ":", err4)
		}

		_, err5 := io.Copy(fout, logFile)
		if err5 != nil {
			log.Fatalln("Failed to copy untrimmed parts of log file", os.Stdout, ":", err5)
		}

		if err6 := os.Remove(fi.Name()); err6 != nil {
			log.Fatalln("Failed to remove untrimmed log file", os.Stdout, ":", err6)
		}

		if err7 := os.Rename("logtmp.txt", fi.Name()); err7 != nil {
			log.Fatalln("Failed to rename trimmed log file", os.Stdout, ":", err7)
		}
	}
}
