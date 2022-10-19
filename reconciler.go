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
	"context"
	"sync"
	"time"

	irc "github.com/fluffle/goirc/client"
	"github.com/google/alertmanager-irc-relay/logging"
)

const (
	ircJoinWaitSecs         = 10
	ircJoinMaxBackoffSecs   = 300
	ircJoinBackoffResetSecs = 1800
)

type channelState struct {
	channel IRCChannel
	chanservName string
	client  *irc.Conn

	delayer    Delayer
	timeTeller TimeTeller

	joinDone chan struct{} // joined when channel is closed
	joined   bool

	joinUnsetSignal chan bool

	mu sync.Mutex
}

func newChannelState(channel *IRCChannel, client *irc.Conn, delayerMaker DelayerMaker, timeTeller TimeTeller, chanservName string) *channelState {
	delayer := delayerMaker.NewDelayer(ircJoinMaxBackoffSecs, ircJoinBackoffResetSecs, time.Second)

	return &channelState{
		channel:         *channel,
		client:          client,
		delayer:         delayer,
		timeTeller:      timeTeller,
		joinDone:        make(chan struct{}),
		joined:          false,
		joinUnsetSignal: make(chan bool),
		chanservName:    chanservName,
	}
}

func (c *channelState) JoinDone() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.joinDone
}

func (c *channelState) SetJoined() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.joined == true {
		logging.Warn("Not setting JOIN state on channel %s: already set", c.channel.Name)
		return
	}

	logging.Info("Setting JOIN state on channel %s", c.channel.Name)
	c.joined = true
	close(c.joinDone)
}

func (c *channelState) UnsetJoined() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.joined == false {
		logging.Warn("Not removing JOIN state on channel %s: already not set", c.channel.Name)
		return
	}

	logging.Info("Removing JOIN state on channel %s", c.channel.Name)
	c.joined = false
	c.joinDone = make(chan struct{})

	// eventually poke monitor routine
	select {
	case c.joinUnsetSignal <- true:
	default:
	}
}

func (c *channelState) join(ctx context.Context) {
	logging.Info("Channel %s monitor: waiting to join", c.channel.Name)
	if ok := c.delayer.DelayContext(ctx); !ok {
		return
	}

	// Try to unban ourselves, just in case
	c.client.Privmsgf(c.chanservName, "UNBAN %s", c.channel.Name)

	c.client.Join(c.channel.Name, c.channel.Password)
	logging.Info("Channel %s monitor: join request sent", c.channel.Name)

	select {
	case <-c.JoinDone():
		logging.Info("Channel %s monitor: join succeeded", c.channel.Name)
	case <-c.timeTeller.After(ircJoinWaitSecs * time.Second):
		logging.Warn("Channel %s monitor: could not join after %d seconds, will retry", c.channel.Name, ircJoinWaitSecs)
	case <-ctx.Done():
		logging.Info("Channel %s monitor: context canceled while waiting for join", c.channel.Name)
	}
}

func (c *channelState) monitorJoinUnset(ctx context.Context) {
	select {
	case <-c.joinUnsetSignal:
		logging.Info("Channel %s monitor: channel no longer joined", c.channel.Name)
	case <-ctx.Done():
		logging.Info("Channel %s monitor: context canceled while monitoring", c.channel.Name)
	}
}

func (c *channelState) Monitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	joined := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.joined
	}

	for ctx.Err() != context.Canceled {
		if !joined() {
			c.join(ctx)
		} else {
			c.monitorJoinUnset(ctx)
		}
	}
}

type ChannelReconciler struct {
	preJoinChannels []IRCChannel
	client          *irc.Conn

	delayerMaker DelayerMaker
	timeTeller   TimeTeller

	channels map[string]*channelState
	chanservName  string

	stopCtx       context.Context
	stopCtxCancel context.CancelFunc
	stopWg        sync.WaitGroup

	mu sync.Mutex
}

func NewChannelReconciler(config *Config, client *irc.Conn, delayerMaker DelayerMaker, timeTeller TimeTeller) *ChannelReconciler {
	reconciler := &ChannelReconciler{
		preJoinChannels: config.IRCChannels,
		client:          client,
		delayerMaker:    delayerMaker,
		timeTeller:      timeTeller,
		channels:        make(map[string]*channelState),
		chanservName:    config.ChanservName,
	}

	reconciler.registerHandlers()

	return reconciler
}

func (r *ChannelReconciler) registerHandlers() {
	r.client.HandleFunc(irc.JOIN,
		func(_ *irc.Conn, line *irc.Line) {
			r.HandleJoin(line.Nick, line.Args[0])
		})

	r.client.HandleFunc(irc.KICK,
		func(_ *irc.Conn, line *irc.Line) {
			r.HandleKick(line.Args[1], line.Args[0])
		})
}

func (r *ChannelReconciler) HandleJoin(nick string, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if nick != r.client.Me().Nick {
		// received join info for somebody else
		return
	}
	logging.Info("Received JOIN confirmation for channel %s", channel)

	c, ok := r.channels[channel]
	if !ok {
		logging.Warn("Not processing JOIN for channel %s: unknown channel", channel)
		return
	}
	c.SetJoined()
}

func (r *ChannelReconciler) HandleKick(nick string, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if nick != r.client.Me().Nick {
		// received kick info for somebody else
		return
	}
	logging.Info("Received KICK for channel %s", channel)

	c, ok := r.channels[channel]
	if !ok {
		logging.Warn("Not processing KICK for channel %s: unknown channel", channel)
		return
	}
	c.UnsetJoined()
}

func (r *ChannelReconciler) unsafeAddChannel(channel *IRCChannel) *channelState {
	c := newChannelState(channel, r.client, r.delayerMaker, r.timeTeller, r.chanservName)

	r.stopWg.Add(1)
	go c.Monitor(r.stopCtx, &r.stopWg)

	r.channels[channel.Name] = c
	return c
}

func (r *ChannelReconciler) JoinChannel(channel string) (bool, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.channels[channel]
	if !ok {
		logging.Info("Request to JOIN new channel %s", channel)
		c = r.unsafeAddChannel(&IRCChannel{Name: channel})
	}

	select {
	case <-c.JoinDone():
		return true, nil
	default:
		return false, c.JoinDone()
	}
}

func (r *ChannelReconciler) unsafeStop() {
	if r.stopCtxCancel == nil {
		// calling stop before first start, ignoring
		return
	}
	r.stopCtxCancel()
	r.stopWg.Wait()
	r.channels = make(map[string]*channelState)
}

func (r *ChannelReconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.unsafeStop()
}

func (r *ChannelReconciler) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.unsafeStop()

	r.stopCtx, r.stopCtxCancel = context.WithCancel(ctx)

	for _, channel := range r.preJoinChannels {
		r.unsafeAddChannel(&channel)
	}
}
