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
	"log"
	"sync"
	"time"

	irc "github.com/fluffle/goirc/client"
)

type channelState struct {
	Channel        IRCChannel
	BackoffCounter Delayer
}

type ChannelReconciler struct {
	preJoinChannels []IRCChannel
	client          *irc.Conn

	delayerMaker DelayerMaker

	channels map[string]*channelState

	stopCtx       context.Context
	stopCtxCancel context.CancelFunc
	stopWg        sync.WaitGroup

	mu sync.Mutex
}

func NewChannelReconciler(config *Config, client *irc.Conn, delayerMaker DelayerMaker) *ChannelReconciler {
	reconciler := &ChannelReconciler{
		preJoinChannels: config.IRCChannels,
		client:          client,
		delayerMaker:    delayerMaker,
		channels:        make(map[string]*channelState),
	}

	reconciler.registerHandlers()

	return reconciler
}

func (r *ChannelReconciler) registerHandlers() {
	r.client.HandleFunc(irc.KICK,
		func(_ *irc.Conn, line *irc.Line) {
			r.HandleKick(line.Args[1], line.Args[0])
		})
}

func (r *ChannelReconciler) HandleKick(nick string, channel string) {
	if nick != r.client.Me().Nick {
		// received kick info for somebody else
		return
	}
	state, ok := r.channels[channel]
	if !ok {
		log.Printf("Being kicked out of non-joined channel (%s), ignoring", channel)
		return
	}
	log.Printf("Being kicked out of %s, re-joining", channel)
	go func() {
		if ok := state.BackoffCounter.DelayContext(r.stopCtx); !ok {
			return
		}
		r.client.Join(state.Channel.Name, state.Channel.Password)
	}()
}

func (r *ChannelReconciler) CleanupChannels() {
	log.Printf("Deregistering all channels.")
	r.channels = make(map[string]*channelState)
}

func (r *ChannelReconciler) JoinChannel(channel *IRCChannel) {
	if _, joined := r.channels[channel.Name]; joined {
		return
	}
	log.Printf("Joining %s", channel.Name)
	r.client.Join(channel.Name, channel.Password)
	state := &channelState{
		Channel: *channel,
		BackoffCounter: r.delayerMaker.NewDelayer(
			ircConnectMaxBackoffSecs, ircConnectBackoffResetSecs,
			time.Second),
	}
	r.channels[channel.Name] = state
}

func (r *ChannelReconciler) JoinChannels() {
	for _, channel := range r.preJoinChannels {
		r.JoinChannel(&channel)
	}
}
