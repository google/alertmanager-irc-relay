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

package main

import (
	"bufio"
	"reflect"
	"sync"
	"testing"

	irc "github.com/fluffle/goirc/client"
)

func makeReconcilerTestIRCConfig(IRCPort int) *Config {
	config := makeTestIRCConfig(IRCPort)
	config.IRCChannels = []IRCChannel{
		IRCChannel{Name: "#foo"},
		IRCChannel{Name: "#bar"},
		IRCChannel{Name: "#baz"},
	}
	return config
}

func makeTestReconciler(config *Config) (*ChannelReconciler, chan bool, chan bool) {

	sessionUp := make(chan bool)
	sessionDown := make(chan bool)

	client := irc.Client(makeGOIRCConfig(config))
	client.Config().Flood = true
	client.HandleFunc(irc.CONNECTED,
		func(*irc.Conn, *irc.Line) {
			sessionUp <- true
		})

	client.HandleFunc(irc.DISCONNECTED,
		func(*irc.Conn, *irc.Line) {
			sessionDown <- false
		})

	fakeDelayerMaker := &FakeDelayerMaker{}
	reconciler := NewChannelReconciler(config, client, fakeDelayerMaker)

	return reconciler, sessionUp, sessionDown
}

func TestPreJoinChannels(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeReconcilerTestIRCConfig(port)
	reconciler, sessionUp, sessionDown := makeTestReconciler(config)

	var testStep sync.WaitGroup

	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)

	reconciler.client.Connect()

	<-sessionUp
	reconciler.JoinChannels()

	testStep.Wait()

	reconciler.client.Quit("see ya")
	<-sessionDown

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Did not pre-join channels")
	}
}
