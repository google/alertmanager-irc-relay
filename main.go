// Copyright 2018 Google LLC
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

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {

	configFile := flag.String("config", "", "Config file path.")

	flag.Parse()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Printf("Could not load config: %s", err)
		return
	}

	alertNotices := make(chan AlertNotice, 10)

	ircNotifier, err := NewIRCNotifier(config, alertNotices)
	if err != nil {
		log.Printf("Could not create IRC notifier: %s", err)
		return
	}
	go ircNotifier.Run()

	httpServer, err := NewHTTPServer(config, alertNotices)
	if err != nil {
		log.Printf("Could not create HTTP server: %s", err)
		return
	}
	go httpServer.Run()

	select {
	case <-httpServer.StoppedRunning:
		log.Printf("Http server terminated, exiting")
	case <-ircNotifier.StoppedRunning:
		log.Printf("IRC notifier stopped running, exiting")
	case s := <-signals:
		log.Printf("Received %s, exiting", s)
		ircNotifier.StopRunning <- true
		log.Printf("Waiting for IRC to quit")
		<-ircNotifier.StoppedRunning
	}
}
