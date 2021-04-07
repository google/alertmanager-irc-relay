// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logging

import (
	"flag"
	"fmt"
	"log"
	"os"

	goirc_logging "github.com/fluffle/goirc/logging"
)

type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

var logger Logger

type stdOutLogger struct {
	out *log.Logger
}

var debugFlag = flag.Bool("debug", false, "Enable debug logging.")

func (l stdOutLogger) Debug(f string, a ...interface{}) {
	if *debugFlag {
		l.out.Output(3, fmt.Sprintf("DEBUG "+f, a...))
	}
}

func (l stdOutLogger) Info(f string, a ...interface{}) {
	l.out.Output(3, fmt.Sprintf("INFO "+f, a...))
}

func (l stdOutLogger) Warn(f string, a ...interface{}) {
	l.out.Output(3, fmt.Sprintf("WARN "+f, a...))
}

func (l stdOutLogger) Error(f string, a ...interface{}) {
	l.out.Output(3, fmt.Sprintf("ERROR "+f, a...))
}

func init() {
	logger = stdOutLogger{
		out: log.New(os.Stderr, "", log.Ldate|log.Lmicroseconds|log.Lshortfile),
	}
	goirc_logging.SetLogger(logger)
}

func Debug(f string, a ...interface{}) { logger.Debug(f, a...) }
func Info(f string, a ...interface{})  { logger.Info(f, a...) }
func Warn(f string, a ...interface{})  { logger.Warn(f, a...) }
func Error(f string, a ...interface{}) { logger.Error(f, a...) }
