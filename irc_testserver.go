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
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"

	irc "github.com/fluffle/goirc/client"
)

type LineHandlerFunc func(*bufio.ReadWriter, *irc.Line) error

func hJOIN(conn *bufio.ReadWriter, line *irc.Line) error {
	r := fmt.Sprintf(":foo!foo@example.com JOIN :%s\n", line.Args[0])
	_, err := conn.WriteString(r)
	return err
}

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
	s.lineHandlers["JOIN"] = hJOIN
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
