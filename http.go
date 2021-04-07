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
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/alertmanager-irc-relay/logging"
	"github.com/gorilla/mux"
	promtmpl "github.com/prometheus/alertmanager/template"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	handledAlertGroups = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_handled_alert_groups",
		Help: "Number of alert groups received"},
		[]string{"ircchannel"},
	)
	handledAlerts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_handled_alerts",
		Help: "Number of single alert messages relayed"},
		[]string{"ircchannel"},
	)
	alertHandlingErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_alert_handling_errors",
		Help: "Errors while processing webhook requests"},
		[]string{"ircchannel", "error"},
	)
)

type HTTPListener func(string, http.Handler) error

type HTTPServer struct {
	Addr         string
	Port         int
	formatter    *Formatter
	AlertMsgs    chan AlertMsg
	httpListener HTTPListener
}

func NewHTTPServer(config *Config, alertMsgs chan AlertMsg) (
	*HTTPServer, error) {
	return NewHTTPServerForTesting(config, alertMsgs, http.ListenAndServe)
}

func NewHTTPServerForTesting(config *Config, alertMsgs chan AlertMsg,
	httpListener HTTPListener) (*HTTPServer, error) {
	formatter, err := NewFormatter(config)
	if err != nil {
		return nil, err
	}
	server := &HTTPServer{
		Addr:         config.HTTPHost,
		Port:         config.HTTPPort,
		formatter:    formatter,
		AlertMsgs:    alertMsgs,
		httpListener: httpListener,
	}

	return server, nil
}

func (s *HTTPServer) RelayAlert(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ircChannel := "#" + vars["IRCChannel"]

	body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024*1024*1024))
	if err != nil {
		logging.Error("Could not get body: %s", err)
		alertHandlingErrors.WithLabelValues(ircChannel, "read_body").Inc()
		return
	}

	var alertMessage = promtmpl.Data{}
	if err := json.Unmarshal(body, &alertMessage); err != nil {
		logging.Error("Could not decode request body (%s): %s", err, body)
		alertHandlingErrors.WithLabelValues(ircChannel, "decode_body").Inc()
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(422) // Unprocessable entity
		if err := json.NewEncoder(w).Encode(err); err != nil {
			logging.Error("Could not write decoding error: %s", err)
			return
		}
		return
	}
	handledAlertGroups.WithLabelValues(ircChannel).Inc()
	for _, alertMsg := range s.formatter.GetMsgsFromAlertMessage(
		ircChannel, &alertMessage) {
		select {
		case s.AlertMsgs <- alertMsg:
			handledAlerts.WithLabelValues(ircChannel).Inc()
		default:
			logging.Error("Could not send this alert to the IRC routine: %s",
				alertMsg)
			alertHandlingErrors.WithLabelValues(ircChannel, "internal_comm_channel_full").Inc()
		}
	}
}

func (s *HTTPServer) Run() {
	router := mux.NewRouter().StrictSlash(true)

	router.Path("/metrics").Handler(promhttp.Handler())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.RelayAlert(w, r)
	})
	router.Path("/{IRCChannel}").Handler(handler).Methods("POST")

	listenAddr := strings.Join(
		[]string{s.Addr, strconv.Itoa(s.Port)}, ":")
	logging.Info("Starting HTTP server")
	if err := s.httpListener(listenAddr, router); err != nil {
		logging.Error("Could not start http server: %s", err)
	}
}
