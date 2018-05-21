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
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	promtmpl "github.com/prometheus/alertmanager/template"
	"strconv"
	"strings"
	"text/template"
)

type HTTPListener func(string, http.Handler) error

type HTTPServer struct {
	StoppedRunning chan bool
	Addr           string
	Port           int
	NoticeTemplate *template.Template
	NoticeOnce     bool
	AlertNotices   chan AlertNotice
	httpListener   HTTPListener
}

func NewHTTPServer(config *Config, alertNotices chan AlertNotice) (
	*HTTPServer, error) {
	return NewHTTPServerForTesting(config, alertNotices, http.ListenAndServe)
}

func NewHTTPServerForTesting(config *Config, alertNotices chan AlertNotice,
	httpListener HTTPListener) (*HTTPServer, error) {
	tmpl, err := template.New("notice").Parse(config.NoticeTemplate)
	if err != nil {
		return nil, err
	}
	server := &HTTPServer{
		StoppedRunning: make(chan bool),
		Addr:           config.HTTPHost,
		Port:           config.HTTPPort,
		NoticeTemplate: tmpl,
		NoticeOnce:     config.NoticeOnce,
		AlertNotices:   alertNotices,
		httpListener:   httpListener,
	}

	return server, nil
}

func (server *HTTPServer) FormatNotice(data interface{}) string {
	output := bytes.Buffer{}
	var msg string
	if err := server.NoticeTemplate.Execute(&output, data); err != nil {
		msg_bytes, _ := json.Marshal(data)
		msg = string(msg_bytes)
		log.Printf("Could not apply notice template on alert (%s): %s",
			err, msg)
		log.Printf("Sending raw alert")
	} else {
		msg = output.String()
	}
	return msg
}

func (server *HTTPServer) GetNoticesFromAlertMessage(ircChannel string,
	data *promtmpl.Data) []AlertNotice {
	notices := []AlertNotice{}
	if server.NoticeOnce {
		msg := server.FormatNotice(data)
		notices = append(notices,
			AlertNotice{Channel: ircChannel, Alert: msg})
	} else {
		for _, alert := range data.Alerts {
			msg := server.FormatNotice(alert)
			notices = append(notices,
				AlertNotice{Channel: ircChannel, Alert: msg})
		}
	}
	return notices
}

func (server *HTTPServer) RelayAlert(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ircChannel := "#" + vars["IRCChannel"]

	body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024*1024*1024))
	if err != nil {
		log.Printf("Could not get body: %s", err)
		return
	}

	var alertMessage = promtmpl.Data{}
	if err := json.Unmarshal(body, &alertMessage); err != nil {
		log.Printf("Could not decode request body (%s): %s", err, body)

		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(422) // Unprocessable entity
		if err := json.NewEncoder(w).Encode(err); err != nil {
			log.Printf("Could not write decoding error: %s", err)
			return
		}
		return
	}
	for _, alertNotice := range server.GetNoticesFromAlertMessage(
		ircChannel, &alertMessage) {
		select {
		case server.AlertNotices <- alertNotice:
		default:
			log.Printf("Could not send this alert to the IRC routine: %s",
				alertNotice)
		}
	}
}

func (server *HTTPServer) Run() {
	router := mux.NewRouter().StrictSlash(true)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.RelayAlert(w, r)
	})
	router.Path("/{IRCChannel}").Handler(handler).Methods("POST")

	listenAddr := strings.Join(
		[]string{server.Addr, strconv.Itoa(server.Port)}, ":")
	log.Printf("Starting HTTP server")
	if err := server.httpListener(listenAddr, router); err != nil {
		log.Printf("Could not start http server: %s", err)
	}
	server.StoppedRunning <- true
}
