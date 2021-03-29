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
	"context"
	"flag"
	"log"
	"sync"
	"syscall"
)

func main() {

	configFile := flag.String("config", "", "Config file path.")

	flag.Parse()

	ctx, _ := WithSignal(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	stopWg := sync.WaitGroup{}

	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Printf("Could not load config: %s", err)
		return
	}

	alertMsgs := make(chan AlertMsg, config.AlertBufferSize)

	stopWg.Add(1)
	ircNotifier, err := NewIRCNotifier(config, alertMsgs, &BackoffMaker{}, &RealTime{})
	if err != nil {
		log.Printf("Could not create IRC notifier: %s", err)
		return
	}
	go ircNotifier.Run(ctx, &stopWg)

	httpServer, err := NewHTTPServer(config, alertMsgs)
	if err != nil {
		log.Printf("Could not create HTTP server: %s", err)
		return
	}
	go httpServer.Run()

	stopWg.Wait()
}
