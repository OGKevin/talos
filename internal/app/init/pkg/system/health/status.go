/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package health

import (
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"

	"github.com/talos-systems/talos/internal/app/init/proto"
)

// Status of the healthcheck
type Status struct {
	Healthy     *bool
	LastChange  time.Time
	LastMessage string
}

// StateChange is used to notify about status changes
type StateChange struct {
	Old Status
	New Status
}

// State provides proper locking around health state
type State struct {
	sync.Mutex

	status      Status
	subscribers []chan<- StateChange
}

// Update health status (locked)
func (state *State) Update(healthy bool, message string) {
	state.Lock()

	oldStatus := state.status
	notify := false

	if state.status.Healthy == nil || *state.status.Healthy != healthy {
		notify = true
		state.status.Healthy = &healthy
		state.status.LastChange = time.Now()
	}
	state.status.LastMessage = message

	newStatus := state.status

	var subscribers []chan<- StateChange
	if notify {
		subscribers = append([]chan<- StateChange(nil), state.subscribers...)
	}

	state.Unlock()

	if notify {
		for _, ch := range subscribers {
			select {
			case ch <- StateChange{oldStatus, newStatus}:
			default:
				// drop messages to clients which don't consume them
			}
		}
	}
}

// Subscribe for the notifications on state changes
func (state *State) Subscribe(ch chan<- StateChange) {
	state.Lock()
	defer state.Unlock()

	state.subscribers = append(state.subscribers, ch)
}

// Unsubscribe from state changes
func (state *State) Unsubscribe(ch chan<- StateChange) {
	state.Lock()
	defer state.Unlock()

	for i := range state.subscribers {
		if state.subscribers[i] == ch {
			state.subscribers[i] = state.subscribers[len(state.subscribers)-1]
			state.subscribers[len(state.subscribers)-1] = nil
			state.subscribers = state.subscribers[:len(state.subscribers)-1]
		}
	}
}

// Init health status (locked)
func (state *State) Init() {
	state.Lock()
	defer state.Unlock()

	state.status.LastMessage = "Unknown"
	state.status.LastChange = time.Now()
	state.status.Healthy = nil
}

// Get returns health status (locked)
func (state *State) Get() Status {
	state.Lock()
	defer state.Unlock()

	return state.status
}

// AsProto returns protobuf-ready health state
func (state *State) AsProto() *proto.ServiceHealth {
	status := state.Get()

	// nolint: errcheck
	tspb, _ := ptypes.TimestampProto(status.LastChange)

	return &proto.ServiceHealth{
		Unknown:     status.Healthy == nil,
		Healthy:     status.Healthy != nil && *status.Healthy,
		LastMessage: status.LastMessage,
		LastChange:  tspb,
	}
}