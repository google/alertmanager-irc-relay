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
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	irc "github.com/fluffle/goirc/client"
)

type LineHandlerFunc func(*bufio.ReadWriter, *irc.Line) error

func hUSER(conn *bufio.ReadWriter, line *irc.Line) error {
	r := fmt.Sprintf(":example.com 001 %s :Welcome\n", line.Args[0])
	_, err := conn.WriteString(r)
	return err
}

func hQUIT(conn *bufio.ReadWriter, line *irc.Line) error {
	return fmt.Errorf("client asked to terminate")
}

type closeEarlyHandler func()

type testServer struct {
	net.Listener
	Client net.Conn

	ServingWaitGroup     sync.WaitGroup
	ConnectionsWaitGroup sync.WaitGroup

	lineHandlersMu sync.Mutex
	lineHandlers   map[string]LineHandlerFunc

	Log []string

	closeEarlyMu sync.Mutex
	closeEarlyHandler
}

func (s *testServer) setDefaultHandlers() {
	if s.lineHandlers == nil {
		s.lineHandlers = make(map[string]LineHandlerFunc)
	}
	s.lineHandlers["USER"] = hUSER
	s.lineHandlers["QUIT"] = hQUIT
}

func (s *testServer) getHandler(cmd string) LineHandlerFunc {
	s.lineHandlersMu.Lock()
	defer s.lineHandlersMu.Unlock()
	return s.lineHandlers[cmd]
}

func (s *testServer) SetHandler(cmd string, h LineHandlerFunc) {
	s.lineHandlersMu.Lock()
	defer s.lineHandlersMu.Unlock()
	if h == nil {
		delete(s.lineHandlers, cmd)
	} else {
		s.lineHandlers[cmd] = h
	}
}

func (s *testServer) handleLine(conn *bufio.ReadWriter, line *irc.Line) error {
	s.Log = append(s.Log, strings.Trim(line.Raw, " \r\n"))
	handler := s.getHandler(line.Cmd)
	if handler == nil {
		log.Printf("=Server= No handler for command '%s', skipping", line.Cmd)
		return nil
	}
	return handler(conn, line)
}

func (s *testServer) handleConnection(conn net.Conn) {
	defer func() {
		s.Client = nil
		conn.Close()
		s.ConnectionsWaitGroup.Done()
	}()
	bufConn := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	for {
		msg, err := bufConn.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				log.Printf("=Server= Client %s disconnected", conn.RemoteAddr().String())
			} else {
				log.Printf("=Server= Could not read from %s: %s", conn.RemoteAddr().String(), err)
			}
			return
		}
		log.Printf("=Server= Received %s", msg)
		line := irc.ParseLine(string(msg))
		if line == nil {
			log.Printf("=Server= Could not parse received line")
			continue
		}
		err = s.handleLine(bufConn, line)
		if err != nil {
			log.Printf("=Server= Closing connection: %s", err)
			return
		}
		bufConn.Flush()
	}
}

func (s *testServer) SetCloseEarly(h closeEarlyHandler) {
	s.closeEarlyMu.Lock()
	defer s.closeEarlyMu.Unlock()
	s.closeEarlyHandler = h
}

func (s *testServer) handleCloseEarly(conn net.Conn) bool {
	s.closeEarlyMu.Lock()
	defer s.closeEarlyMu.Unlock()
	if s.closeEarlyHandler == nil {
		return false
	}
	log.Printf("=Server= Closing connection early")
	conn.Close()
	s.closeEarlyHandler()
	return true
}

func (s *testServer) Serve() {
	defer s.ServingWaitGroup.Done()
	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			log.Printf("=Server= Stopped accepting new connections")
			return
		}
		log.Printf("=Server= New client connected from %s", conn.RemoteAddr().String())
		if s.handleCloseEarly(conn) {
			continue
		}
		s.Client = conn
		s.ConnectionsWaitGroup.Add(1)
		s.handleConnection(conn)
	}
}

func (s *testServer) Stop() {
	s.Listener.Close()
	s.ServingWaitGroup.Wait()
	s.ConnectionsWaitGroup.Wait()
}

func makeTestServer(t *testing.T) (*testServer, int) {
	server := new(testServer)
	server.Log = make([]string, 0)
	server.setDefaultHandlers()

	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("=Server= Could not resolve tcp addr: %s", err)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("=Server= Could not create listener: %s", err)
	}
	addr = listener.Addr().(*net.TCPAddr)
	log.Printf("=Server= Test server listening on %s", addr.String())

	server.Listener = listener

	server.ServingWaitGroup.Add(1)
	go func() {
		server.Serve()
	}()

	addr = listener.Addr().(*net.TCPAddr)
	return server, addr.Port
}

type FakeDelayer struct {
	DelayOnChan bool
	StopDelay   chan bool
}

func (f *FakeDelayer) Delay() {
	f.DelayContext(context.Background())
}

func (f *FakeDelayer) DelayContext(ctx context.Context) bool {
	log.Printf("Faking Backoff")
	if f.DelayOnChan {
		log.Printf("Waiting StopDelay signal")
		<-f.StopDelay
		log.Printf("Received StopDelay signal")
	}
	return true
}

func makeTestIRCConfig(IRCPort int) *Config {
	return &Config{
		IRCNick:     "foo",
		IRCNickPass: "",
		IRCHost:     "127.0.0.1",
		IRCPort:     IRCPort,
		IRCUseSSL:   false,
		IRCChannels: []IRCChannel{
			IRCChannel{Name: "#foo"},
			IRCChannel{Name: "#bar"},
			IRCChannel{Name: "#baz"},
		},
		UsePrivmsg: false,
	}
}

func makeTestNotifier(t *testing.T, config *Config) (*IRCNotifier, chan AlertMsg, context.CancelFunc, *sync.WaitGroup) {
	alertMsgs := make(chan AlertMsg)
	ctx, cancel := context.WithCancel(context.Background())
	stopWg := sync.WaitGroup{}
	stopWg.Add(1)
	notifier, err := NewIRCNotifier(ctx, &stopWg, config, alertMsgs)
	if err != nil {
		t.Fatal(fmt.Sprintf("Could not create IRC notifier: %s", err))
	}
	notifier.Client.Config().Flood = true
	notifier.BackoffCounter = &FakeDelayer{
		DelayOnChan: false,
		StopDelay:   make(chan bool),
	}

	return notifier, alertMsgs, cancel, &stopWg
}

func TestPreJoinChannels(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, _, cancel, _ := makeTestNotifier(t, config)

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
	go notifier.Run()

	testStep.Wait()

	cancel()
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

func TestServerPassword(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCHostPass = "hostsecret"
	notifier, _, cancel, _ := makeTestNotifier(t, config)

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
	go notifier.Run()

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"PASS hostsecret",
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Did not send IRC server password")
	}
}

func TestSendAlertOnPreJoinedChannel(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, alertMsgs, cancel, _ := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foo"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure we
	// check for no re-join attempt.
	joinedHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		if line.Args[0] == testChannel {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinedHandler)

	testStep.Add(1)
	go notifier.Run()

	testStep.Wait()

	server.SetHandler("JOIN", nil)

	noticeHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("NOTICE", noticeHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
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
	notifier, alertMsgs, cancel, _ := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foo"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure we
	// check for no re-join attempt.
	joinedHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		if line.Args[0] == testChannel {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinedHandler)

	testStep.Add(1)
	go notifier.Run()

	testStep.Wait()

	server.SetHandler("JOIN", nil)

	privmsgHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("PRIVMSG", privmsgHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
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
	notifier, alertMsgs, cancel, _ := makeTestNotifier(t, config)

	var testStep sync.WaitGroup

	testChannel := "#foobar"
	testMessage := "test message"

	// Send the alert after configured channels have joined, to ensure log
	// ordering.
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run()

	testStep.Wait()

	server.SetHandler("JOIN", nil)

	noticeHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		testStep.Done()
		return nil
	}
	server.SetHandler("NOTICE", noticeHandler)

	testStep.Add(1)
	alertMsgs <- AlertMsg{Channel: testChannel, Alert: testMessage}

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		// #foobar joined before sending message
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
	notifier, alertMsgs, cancel, _ := makeTestNotifier(t, config)

	var testStep, holdUserStep sync.WaitGroup

	testChannel := "#foo"
	disconnectedTestMessage := "disconnected test message"
	connectedTestMessage := "connected test message"

	// First send an alert while the session is not established.
	testStep.Add(1)
	holdUserStep.Add(1)
	holdUser := func(conn *bufio.ReadWriter, line *irc.Line) error {
		log.Printf("=Server= Wait before completing session")
		testStep.Wait()
		log.Printf("=Server= Completing session")
		holdUserStep.Done()
		return hUSER(conn, line)
	}
	server.SetHandler("USER", holdUser)

	go notifier.Run()

	// Alert channels is not consumed while disconnected
	select {
	case alertMsgs <- AlertMsg{Channel: testChannel, Alert: disconnectedTestMessage}:
		t.Error("Alert consumed while disconnected")
	default:
	}

	testStep.Done()
	holdUserStep.Wait()

	// Make sure session is established by checking that pre-joined
	// channels are there.
	testStep.Add(1)
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			testStep.Done()
		}
		return nil
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
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
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
	notifier, _, cancel, _ := makeTestNotifier(t, config)

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
	go notifier.Run()

	// Wait until the last pre-joined channel is seen.
	testStep.Wait()

	// Simulate disconnection.
	testStep.Add(1)
	server.Client.Close()

	// Wait again until the last pre-joined channel is seen.
	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		// Commands from first connection
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		// Commands from reconnection
		"NICK foo",
		"USER foo 12 * :",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
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
	notifier, _, cancel, _ := makeTestNotifier(t, config)
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

	go notifier.Run()

	delayer.StopDelay <- true

	testStep.Wait()

	// We have caused a connection failure, now check for a reconnection
	notifier.Client.Config().SSL = false
	joinStep.Add(1)
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			joinStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinHandler)
	server.SetCloseEarly(nil)

	delayer.StopDelay <- true

	joinStep.Wait()

	cancel()
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
		t.Error("Reconnection did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestIdentify(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCNickPass = "nickpassword"
	notifier, _, cancel, _ := makeTestNotifier(t, config)
	notifier.NickservDelayWait = 0 * time.Second

	var testStep sync.WaitGroup

	// Wait until the last pre-joined channel is seen (joining happens
	// after identification).
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run()

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"PRIVMSG NickServ :IDENTIFY nickpassword",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Identification did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestGhostAndIdentify(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	config.IRCNickPass = "nickpassword"
	notifier, _, cancel, _ := makeTestNotifier(t, config)
	notifier.NickservDelayWait = 0 * time.Second

	var testStep, usedNick, unregisteredNickHandler sync.WaitGroup

	// Trigger 433 for first nick
	usedNick.Add(1)
	unregisteredNickHandler.Add(1)
	nickHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		var err error
		if line.Args[0] == "foo" {
			_, err = conn.WriteString(":example.com 433 * foo :nick in use\n")
		}
		usedNick.Done()
		unregisteredNickHandler.Wait()
		return err
	}
	server.SetHandler("NICK", nickHandler)

	// Wait until the last pre-joined channel is seen (joining happens
	// after identification).
	joinHandler := func(conn *bufio.ReadWriter, line *irc.Line) error {
		// #baz is configured as the last channel to pre-join
		if line.Args[0] == "#baz" {
			testStep.Done()
		}
		return nil
	}
	server.SetHandler("JOIN", joinHandler)

	testStep.Add(1)
	go notifier.Run()

	usedNick.Wait()
	server.SetHandler("NICK", nil)
	unregisteredNickHandler.Done()

	testStep.Wait()

	cancel()
	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"NICK foo^",
		"PRIVMSG NickServ :GHOST foo nickpassword",
		"NICK foo",
		"PRIVMSG NickServ :IDENTIFY nickpassword",
		"JOIN #foo",
		"JOIN #bar",
		"JOIN #baz",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Ghosting did not happen correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}

func TestStopRunningWhenHalfConnected(t *testing.T) {
	server, port := makeTestServer(t)
	config := makeTestIRCConfig(port)
	notifier, _, cancel, stopWg := makeTestNotifier(t, config)

	var testStep, holdQuitWait sync.WaitGroup

	// Send a StopRunning request while the client is connected but the
	// session is not up
	testStep.Add(1)
	holdUser := func(conn *bufio.ReadWriter, line *irc.Line) error {
		log.Printf("=Server= NOT completing session")
		testStep.Done()
		return nil
	}
	server.SetHandler("USER", holdUser)

	// Ignore quit, but wait for it to have deterministic test commands
	holdQuitWait.Add(1)
	holdQuit := func(conn *bufio.ReadWriter, line *irc.Line) error {
		log.Printf("=Server= Ignoring quit")
		holdQuitWait.Done()
		return nil
	}
	server.SetHandler("QUIT", holdQuit)

	go notifier.Run()

	testStep.Wait()

	cancel()

	stopWg.Wait()

	holdQuitWait.Wait()

	// Client has left, cleanup the server side before stopping
	server.Client.Close()

	server.Stop()

	expectedCommands := []string{
		"NICK foo",
		"USER foo 12 * :",
		"QUIT :see ya",
	}

	if !reflect.DeepEqual(expectedCommands, server.Log) {
		t.Error("Alert not sent correctly. Received commands:\n", strings.Join(server.Log, "\n"))
	}
}
