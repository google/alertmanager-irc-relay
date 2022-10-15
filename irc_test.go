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
	"bufio"
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	irc "github.com/fluffle/goirc/client"
	"github.com/google/alertmanager-irc-relay/logging"
)

func makeTestIRCConfig(IRCPort int) *Config {
	return &Config{
		IRCNick:     "foo",
		IRCNickPass: "",
		IRCHost:     "127.0.0.1",
		IRCPort:     IRCPort,
		IRCUseSSL:   false,
		IRCChannels: []IRCChannel{
			IRCChannel{Name: "#foo"},
		},
		UsePrivmsg: false,
		NickservIdentifyPatterns: []string{
			"identify yourself ktnxbye",
		},
		NickservName:    "NickServ",
		ChanservName:    "ChanServ",
	}
}

func makeTestNotifier(t *testing.T, config *Config) (*IRCNotifier, chan AlertMsg, context.Context, context.CancelFunc, *sync.WaitGroup) {
	fakeDelayerMaker := &FakeDelayerMaker{}
	fakeTime := &FakeTime{
		afterChan: make(chan time.Time, 1),
	}
	alertMsgs := make(chan AlertMsg)
	ctx, cancel := context.WithCancel(context.Background())
	stopWg := sync.WaitGroup{}
	stopWg.Add(1)
	notifier, err := NewIRCNotifier(config, alertMsgs, fakeDelayerMaker, fakeTime)
	if err != nil {
		t.Fatal(fmt.Sprintf("Could not create IRC notifier: %s", err))
	}
	notifier.Client.Config().Flood = true

	return notifier, alertMsgs, ctx, cancel, &stopWg
}

func TestServerPassword(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCHostPass = "hostsecret"
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"PASS hostsecret",
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Did not send IRC server password. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestSendAlertOnPreJoinedChannel(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, alertMsgs, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foo"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure we
	// check for no re-join attempt.
	joinedHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		if line.Args[0] == testChannel {
			testStep.Done()
		}
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinedHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	server.SetHandler("JOIN", hJOIN)

	noticeHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("NOTICE", noticeHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"NOTICE #foo :test message",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestUsePrivmsgToSendAlertOnPreJoinedChannel(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.UsePrivmsg = true
	notifier, alertMsgs, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foo"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure we
	// check for no re-join attempt.
	joinedHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		if line.Args[0] == testChannel {
			testStep.Done()
		}
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinedHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	server.SetHandler("JOIN", hJOIN)

	privmsgHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("PRIVMSG", privmsgHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"PRIVMSG #foo :test message",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestSendAlertAndJoinChannel(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, alertMsgs, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foobar"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure log
	// ordering.
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	server.SetHandler("JOIN", hJOIN)

	noticeHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("NOTICE", noticeHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		// #foobar joined before sending message
		"PRIVMSG ChanServ :UNBAN #foobar",
		"JOIN #foobar",
		"NOTICE #foobar :test message",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestSendAlertDisconnected(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, alertMsgs, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep, holdUserStep sync.WaitGroup

	testChannel := "#foo"
	disconnectedTestMessage := "disconnected test message"
	connectedTestMessage := "connected test message"

	// First send an alert while the session is not established.
	testStep.Add(1)
	holdUserStep.Add(1)
	holdUser := func(conn *bufio.ReadWriter, line *irc.Line) error {
		logging.Info("=Server= Wait before completing session")
		testStep.Wait()
		logging.Info("=Server= Completing session")
		holdUserStep.Done()
		return hUSER(conn, line)
	}
	server.SetHandler("USER", holdUser)

	go notifier.Run(ctx, stopWg)

	// Alert channels is not consumed while disconnected
	select {
	case alertMsgs <- AlertMsg{Channel: testChannel, Alert: disconnectedTestMessage}:
		t.Error("Alert consumed while disconnected")
	default:
	}

	testStep.Done()
	holdUserStep.Wait()

	// Make sure session is established by checking that pre-joined
	// channel is there.
	testStep.Add(1)
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Wait()

	// Now send and wait until a notice has been received.
	testStep.Add(1)
	noticeHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("NOTICE", noticeHandler)

	alertMsgs <- AlertMsg{Channel: testChannel, Alert: connectedTestMessage}

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		// Only message sent while being connected is received.
		"NOTICE #foo :connected test message",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestReconnect(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	// Wait until the pre-joined channel is seen.
	testStep.Wait()

	// Simulate disconnection.
	testStep.Add(1)
	server.Client.Close()

	// Wait again until the pre-joined channel is seen.
	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		// Commands from first connection
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		// Commands from reconnection
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Reconnection did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestReconnectNickIdentChange(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	userHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		r := fmt.Sprintf(":example.com 001 %s :Welcome to the Internet Relay Network %s!~%s@example.com\n",
			line.Args[0], line.Args[0], line.Args[0])
		_, err := conn.WriteString(r)
		return err
	}
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("USER", userHandler)
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(2)
	go notifier.Run(ctx, stopWg)

	// Wait until the pre-joined channel is seen.
	testStep.Wait()

	if ident := notifier.IrcConfig.Me.Ident; ident != "~foo" {
		t.Errorf("IRC client failed to learn new nick ident. Using %s", ident)
	}

	// Simulate disconnection.
	testStep.Add(2)
	server.Client.Close()

	// Wait again until the pre-joined channel is seen.
	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		// Commands from first connection
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		// Commands from reconnection
		"NICK foo",
		"USER foo 12 * :", // NOTE: the client didn't used ~foo as its ident
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Reconnection did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestConnectErrorRetry(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	// Attempt SSL handshake. The server does not support it, resulting in
	// a connection error.
	config.IRCUseSSL = true
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)
	// Pilot reconnect attempts via backoff delay to prevent race
	// conditions in the test while we change the components behavior on
	// the fly.
	delayer := notifier.BackoffCounter.(*FakeDelayer)
	delayer.DelayOnChan = true

	var testStep, joinStep sync.WaitGroup

	testStep.Add(1)
	earlyHandler := func() {
		testStep.Done()
	}

	server.SetCloseEarly(earlyHandler)

	go notifier.Run(ctx, stopWg)

	delayer.StopDelay <- true

	testStep.Wait()

	// We have caused a connection failure, now check for a reconnection
	notifier.Client.Config().SSL = false
	joinStep.Add(1)
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		joinStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)
	server.SetCloseEarly(nil)

	delayer.StopDelay <- true

	joinStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Reconnection did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestIdentify(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCNickPass = "nickpassword"
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)
	notifier.NickservDelayWait = 0 * time.Second

	var testStep sync.WaitGroup

	// Trigger NickServ identify request when we see the NICK command
	// Note: We also test formatting cleanup with this message
	nickHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		var err error
		_, err = conn.WriteString(":NickServ!NickServ@services. NOTICE airtest :This nickname is registered. Please choose a different nickname, or \002identify yourself\002 ktnxbye.\n")
		return err
	}
	server.SetHandler("NICK", nickHandler)

	// Wait until the pre-joined channel is seen (joining happens
	// after identification).
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG NickServ :IDENTIFY nickpassword",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Identification did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestGhost(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCNickPass = "nickpassword"
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)
	notifier.NickservDelayWait = 0 * time.Second

	var testStep sync.WaitGroup

	// Trigger 433 for first nick when we see the USER command
	userHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		var err error
		if line.Args[0] == "foo" {
			_, err = conn.WriteString(":example.com 433 * foo :nick in use\n")
		}
		return err
	}
	server.SetHandler("USER", userHandler)

	// Trigger 001 when we see NICK foo^
	nickHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		var err error
		if line.Args[0] == "foo^" {
			_, err = conn.WriteString(":example.com 001 foo^ :Welcome\n")
		}
		return err
	}
	server.SetHandler("NICK", nickHandler)

	// Wait until the pre-joined channel is seen (joining happens
	// after ghosting).
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return hJOIN(conn, line)
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"NICK foo^",
		"PRIVMSG NickServ :GHOST foo nickpassword",
		"NICK foo",
		"PRIVMSG ChanServ :UNBAN #foo",
		"JOIN #foo",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Ghosting did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestStopRunningWhenHalfConnected(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, _, ctx, cancel, stopWg := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	// Send a StopRunning request while the client is connected but the
	// session is not up
	testStep.Add(1)
	holdUser := func(conn *bufio.ReadWriter, line *irc.Line) error {
		logging.Info("=Server= NOT completing session")
		testStep.Done()
		return nil
	}
	server.SetHandler("USER", holdUser)

	go notifier.Run(ctx, stopWg)

	testStep.Wait()

	cancel()
	stopWg.Wait()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}
