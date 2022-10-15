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
	"crypto/tls"
	"strconv"
	"strings"
	"sync"
	"time"

	irc "github.com/fluffle/goirc/client"
	"github.com/google/alertmanager-irc-relay/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	connectionTimeoutSecs      = 30
	nickservWaitSecs           = 10
	ircConnectMaxBackoffSecs   = 300
	ircConnectBackoffResetSecs = 1800
)

var (
	ircConnectedGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "irc_connected",
		Help: "Whether the IRC connection is established",
	})
	ircSentMsgs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "irc_sent_msgs",
		Help: "Number of IRC messages sent"},
		[]string{"ircchannel"},
	)
	ircSendMsgErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "irc_send_msg_errors",
		Help: "Errors while sending IRC messages"},
		[]string{"ircchannel", "error"},
	)
)

func loggerHandler(_ *irc.Conn, line *irc.Line) {
	logging.Info("Received: '%s'", line.Raw)
}

func makeGOIRCConfig(config *Config) *irc.Config {
	ircConfig := irc.NewConfig(config.IRCNick)
	ircConfig.Me.Ident = config.IRCNick
	ircConfig.Me.Name = config.IRCRealName
	ircConfig.Server = strings.Join(
		[]string{config.IRCHost, strconv.Itoa(config.IRCPort)}, ":")
	ircConfig.Pass = config.IRCHostPass
	ircConfig.SSL = config.IRCUseSSL
	ircConfig.SSLConfig = &tls.Config{
		ServerName:         config.IRCHost,
		InsecureSkipVerify: !config.IRCVerifySSL,
	}
	ircConfig.PingFreq = time.Duration(config.IRCPingSecs) * time.Second
	ircConfig.Timeout = connectionTimeoutSecs * time.Second
	ircConfig.NewNick = func(n string) string { return n + "^" }

	return ircConfig
}

type IRCNotifier struct {
	// Nick stores the nickname specified in the config, because irc.Client
	// might change its copy.
	Nick         string
	NickPassword string

	NickservName string
	NickservIdentifyPatterns []string

	// As the goirc library might alter the irc.Config created by makeGOIRCConfig,
	// we might also want to keep a reference to the original Config to restore
	// the desired state.
	Config    *Config
	IrcConfig *irc.Config

	Client    *irc.Conn
	AlertMsgs chan AlertMsg

	// irc.Conn has a Connected() method that can tell us wether the TCP
	// connection is up, and thus if we should trigger connect/disconnect.
	// We need to track the session establishment also at a higher level to
	// understand when the server has accepted us and thus when we can join
	// channels, send notices, etc.
	sessionUp         bool
	sessionUpSignal   chan bool
	sessionDownSignal chan bool
	sessionPongSignal chan bool
	sessionPingOnce   sync.Once
	sessionLastPong   time.Time
	sessionWg         sync.WaitGroup

	channelReconciler *ChannelReconciler

	UsePrivmsg bool

	NickservDelayWait time.Duration
	BackoffCounter    Delayer
	timeTeller        TimeTeller
}

func NewIRCNotifier(config *Config, alertMsgs chan AlertMsg, delayerMaker DelayerMaker, timeTeller TimeTeller) (*IRCNotifier, error) {

	ircConfig := makeGOIRCConfig(config)

	client := irc.Client(ircConfig)

	backoffCounter := delayerMaker.NewDelayer(
		ircConnectMaxBackoffSecs, ircConnectBackoffResetSecs,
		time.Second)

	channelReconciler := NewChannelReconciler(config, client, delayerMaker, timeTeller)

	notifier := &IRCNotifier{
		Nick:                     config.IRCNick,
		NickPassword:             config.IRCNickPass,
		NickservName:             config.NickservName,
		NickservIdentifyPatterns: config.NickservIdentifyPatterns,
		Config:                   config,
		IrcConfig:                ircConfig,
		Client:                   client,
		AlertMsgs:                alertMsgs,
		sessionUpSignal:          make(chan bool),
		sessionDownSignal:        make(chan bool),
		sessionPongSignal:        make(chan bool),
		channelReconciler:        channelReconciler,
		UsePrivmsg:               config.UsePrivmsg,
		NickservDelayWait:        nickservWaitSecs * time.Second,
		BackoffCounter:           backoffCounter,
		timeTeller:               timeTeller,
	}

	notifier.registerHandlers()

	return notifier, nil
}

func (n *IRCNotifier) registerHandlers() {
	n.Client.HandleFunc(irc.CONNECTED,
		func(*irc.Conn, *irc.Line) {
			logging.Info("Session established")
			n.sessionUpSignal <- true
		})

	n.Client.HandleFunc(irc.DISCONNECTED,
		func(*irc.Conn, *irc.Line) {
			logging.Info("Disconnected from IRC")
			n.sessionDownSignal <- false
		})

	n.Client.HandleFunc(irc.NOTICE,
		func(_ *irc.Conn, line *irc.Line) {
			n.HandleNotice(line.Nick, line.Text())
		})

	n.Client.HandleFunc(irc.PONG,
		func(_ *irc.Conn, line *irc.Line) {
			n.sessionPongSignal <- true
		})

	for _, event := range []string{"433"} {
		n.Client.HandleFunc(event, loggerHandler)
	}
}

func (n *IRCNotifier) HandleNotice(nick string, msg string) {
	logging.Info("Received NOTICE from %s: %s", nick, msg)
	if strings.ToLower(nick) == "nickserv" {
		n.HandleNickservMsg(msg)
	}
}

func (n *IRCNotifier) HandleNickservMsg(msg string) {
	if n.NickPassword == "" {
		logging.Debug("Skip processing NickServ request, no password configured")
		return
	}

	// Remove most common formatting options from NickServ messages
	cleaner := strings.NewReplacer(
		"\001", "", // bold
		"\002", "", // faint
		"\004", "", // underline
		"\037", "", // underline
	)
	cleanedMsg := cleaner.Replace(msg)

	for _, identifyPattern := range n.NickservIdentifyPatterns {
		logging.Debug("Checking if NickServ message matches identify request '%s'", identifyPattern)
		if strings.Contains(cleanedMsg, identifyPattern) {
			logging.Info("Handling NickServ request to IDENTIFY")
			n.Client.Privmsgf(n.NickservName, "IDENTIFY %s", n.NickPassword)
			return
		}
	}
}

func (n *IRCNotifier) MaybeGhostNick() {
	if n.NickPassword == "" {
		logging.Debug("Skip GHOST check, no password configured")
		return
	}

	currentNick := n.Client.Me().Nick
	if currentNick != n.Nick {
		logging.Info("My nick is '%s', sending GHOST to NickServ to get '%s'",
			currentNick, n.Nick)
		n.Client.Privmsgf(n.NickservName, "GHOST %s %s", n.Nick,
			n.NickPassword)
		time.Sleep(n.NickservDelayWait)

		logging.Info("Changing nick to '%s'", n.Nick)
		n.Client.Nick(n.Nick)
		time.Sleep(n.NickservDelayWait)
	}
}

func (n *IRCNotifier) MaybeWaitForNickserv() {
	if n.NickPassword == "" {
		logging.Debug("Skip NickServ wait, no password configured")
		return
	}

	// Very lazy/optimistic, but this is good enough for my irssi config,
	// so it should work here as well.
	logging.Info("Waiting for NickServ to notice us and issue an identify request")
	time.Sleep(n.NickservDelayWait)
}

func (n *IRCNotifier) ChannelJoined(ctx context.Context, channel string) bool {

	isJoined, waitJoined := n.channelReconciler.JoinChannel(channel)
	if isJoined {
		return true
	}

	select {
	case <-waitJoined:
		return true
	case <-n.timeTeller.After(ircJoinWaitSecs * time.Second):
		logging.Warn("Channel %s not joined after %d seconds, giving bad news to caller", channel, ircJoinWaitSecs)
		return false
	case <-ctx.Done():
		logging.Info("Context canceled while waiting for join on channel %s", channel)
		return false
	}
}

func (n *IRCNotifier) SendAlertMsg(ctx context.Context, alertMsg *AlertMsg) {
	if !n.sessionUp {
		logging.Error("Cannot send alert to %s : IRC not connected", alertMsg.Channel)
		ircSendMsgErrors.WithLabelValues(alertMsg.Channel, "not_connected").Inc()
		return
	}
	if !n.ChannelJoined(ctx, alertMsg.Channel) {
		logging.Error("Cannot send alert to %s : cannot join channel", alertMsg.Channel)
		ircSendMsgErrors.WithLabelValues(alertMsg.Channel, "not_joined").Inc()
		return
	}

	if n.UsePrivmsg {
		n.Client.Privmsg(alertMsg.Channel, alertMsg.Alert)
	} else {
		n.Client.Notice(alertMsg.Channel, alertMsg.Alert)
	}
	ircSentMsgs.WithLabelValues(alertMsg.Channel).Inc()
}

func (n *IRCNotifier) ShutdownPhase() {
	if n.sessionUp {
		logging.Info("IRC client connected, quitting")
		n.Client.Quit("see ya")

		logging.Info("Wait for IRC disconnect to complete")
		select {
		case <-n.sessionDownSignal:
		case <-n.sessionPongSignal:
		case <-n.timeTeller.After(n.Client.Config().Timeout):
			logging.Warn("Timeout while waiting for IRC disconnect to complete, stopping anyway")
		}
		n.sessionWg.Done()
	}
	logging.Info("IRC shutdown complete")
}

func (n *IRCNotifier) ConnectedPhase(ctx context.Context) {
	select {
	case alertMsg := <-n.AlertMsgs:
		n.SendAlertMsg(ctx, &alertMsg)
	case <-n.sessionPongSignal:
		logging.Debug("Received a PONG message; prev PONG was at %v", n.sessionLastPong)
		n.sessionLastPong = time.Now()
	case <-time.After(2*n.IrcConfig.PingFreq - time.Since(n.sessionLastPong)):
		// Calling n.Client.Close() will trigger n.sessionDownSignal. However, as
		// this also dispatches a hook, which we will catch as sessionDownSignal,
		// this needs to be done in a concurrent fashion if we don't want to
		// deadlock ourself.
		//
		// Furthermore, as this time.After(...) interval is now zero, it will also
		// trigger when visiting this select the next time. To mitigate multiple
		// Close() calls, it is wrapped within an sync.Once which will be reset
		// during SetupPhase's sessionUpSignal.
		n.sessionPingOnce.Do(func() {
			logging.Error("Haven't received a PONG after twice the PING period")
			go n.Client.Close()
		})
	case <-n.sessionDownSignal:
		n.sessionUp = false
		n.sessionWg.Done()
		n.channelReconciler.Stop()
		n.Client.Quit("see ya")
		ircConnectedGauge.Set(0)
	case <-ctx.Done():
		logging.Info("IRC routine asked to terminate")
	}
}

func (n *IRCNotifier) SetupPhase(ctx context.Context) {
	if !n.Client.Connected() {
		if n.IrcConfig.Me.Ident != n.Config.IRCNick {
			logging.Debug("Restoring IRC nick from %s to %s", n.IrcConfig.Me.Ident, n.Config.IRCNick)
			n.IrcConfig.Me.Ident = n.Config.IRCNick
		}

		logging.Info("Connecting to IRC %s", n.Client.Config().Server)
		if ok := n.BackoffCounter.DelayContext(ctx); !ok {
			return
		}
		if err := n.Client.ConnectContext(WithWaitGroup(ctx, &n.sessionWg)); err != nil {
			logging.Error("Could not connect to IRC: %s", err)
			return
		}
		logging.Info("Connected to IRC server, waiting to establish session")
	}
	select {
	case <-n.sessionUpSignal:
		n.sessionUp = true
		n.sessionPingOnce = sync.Once{}
		n.sessionLastPong = time.Now()
		n.sessionWg.Add(1)
		n.MaybeGhostNick()
		n.MaybeWaitForNickserv()
		n.channelReconciler.Start(ctx)
		ircConnectedGauge.Set(1)
	case <-n.sessionDownSignal:
		logging.Warn("Receiving a session down before the session is up, this is odd")
	case <-n.sessionPongSignal:
		logging.Warn("Receiving a PONG before the session is up, this is odd")
	case <-ctx.Done():
		logging.Info("IRC routine asked to terminate")
	}
}

func (n *IRCNotifier) Run(ctx context.Context, stopWg *sync.WaitGroup) {
	defer stopWg.Done()

	for ctx.Err() != context.Canceled {
		if !n.sessionUp {
			n.SetupPhase(ctx)
		} else {
			n.ConnectedPhase(ctx)
		}
	}
	n.ShutdownPhase()
}
