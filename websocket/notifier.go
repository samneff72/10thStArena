// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Publish-subscribe model for nonblocking notification of server events to websocket clients.

package websocket

import (
	"log"
	"sync"
	"time"
)

// Allow the listeners to buffer a small number of notifications to streamline delivery.
const notifyBufferSize = 5

// listenerBlockedTimeout is how long a listener may stay full before it is dropped.
//
// A listener goes briefly full whenever its reader is mid-write, which is normal and must
// not cost anyone their subscription -- hence a threshold measured in tens of seconds
// rather than in missed messages. Past it, the reader is not coming back on any timescale
// that matters to a field, and every subsequent event would log another failure.
//
// This is a backstop, not the cure. The reason a reader stops draining is a websocket write
// that never returns, which the write deadline in websocket.go bounds directly.
const listenerBlockedTimeout = 30 * time.Second

type Notifier struct {
	messageType     string
	messageProducer func() any
	listeners       map[chan messageEnvelope]*listenerState
	mutex           sync.Mutex

	// now is the clock, injectable so tests can drive the timeout without sleeping.
	now func() time.Time
}

// listenerState tracks how long a listener has been unable to accept a message.
type listenerState struct {
	blockedSince time.Time // zero while the listener is keeping up
	reported     bool      // whether the current blocked spell has been logged
}

type messageEnvelope struct {
	messageType string
	messageBody any
}

func NewNotifier(messageType string, messageProducer func() any) *Notifier {
	notifier := &Notifier{messageType: messageType, messageProducer: messageProducer, now: time.Now}
	notifier.listeners = make(map[chan messageEnvelope]*listenerState)
	return notifier
}

// Calls the messageProducer function and sends a message containing the results to all registered listeners, and cleans
// up any listeners that have closed.
func (notifier *Notifier) Notify() {
	notifier.NotifyWithMessage(notifier.getMessageBody())
}

// Sends the given message to all registered listeners, and cleans up any listeners that have closed. If there is a
// messageProducer function defined it is ignored.
func (notifier *Notifier) NotifyWithMessage(messageBody any) {
	notifier.mutex.Lock()
	defer notifier.mutex.Unlock()

	message := messageEnvelope{messageType: notifier.messageType, messageBody: messageBody}
	for listener, state := range notifier.listeners {
		notifier.notifyListener(listener, state, message)
	}
}

func (notifier *Notifier) notifyListener(
	listener chan messageEnvelope, state *listenerState, message messageEnvelope,
) {
	defer func() {
		// If channel is closed sending to it will cause a panic; recover and remove it from the list.
		if r := recover(); r != nil {
			delete(notifier.listeners, listener)
		}
	}()

	// Do a non-blocking send. This guarantees that sending notifications won't interrupt the main event loop,
	// at the risk of clients missing some messages if they don't read them all promptly.
	select {
	case listener <- message:
		// The notification was sent and received successfully.
		state.blockedSince = time.Time{}
		state.reported = false
		return
	default:
	}

	// Full. Briefly, that is ordinary -- the reader is mid-write. Sustained, it means the
	// reader is gone, and every event from here would log another line.
	now := notifier.now()
	if state.blockedSince.IsZero() {
		state.blockedSince = now
	}
	if !state.reported {
		log.Printf("Failed to send a '%s' notification due to blocked listener.", notifier.messageType)
		state.reported = true
	}
	if now.Sub(state.blockedSince) >= listenerBlockedTimeout {
		// Dropped from the map, not closed: the reader owns the channel and closes it on
		// its way out. Closing it here would panic that goroutine on its own deferred close.
		delete(notifier.listeners, listener)
		log.Printf(
			"Dropping a '%s' listener blocked for over %s; its client is not reading.",
			notifier.messageType, listenerBlockedTimeout,
		)
	}
}

// Registers and returns a channel that can be read from to receive notification messages. The caller is
// responsible for closing the channel, which will cause it to be reaped from the list of listeners.
func (notifier *Notifier) listen() chan messageEnvelope {
	notifier.mutex.Lock()
	defer notifier.mutex.Unlock()

	listener := make(chan messageEnvelope, notifyBufferSize)
	notifier.listeners[listener] = &listenerState{}
	return listener
}

// Invokes the message producer to get the message, or returns nil if no producer is defined.
func (notifier *Notifier) getMessageBody() any {
	if notifier.messageProducer == nil {
		return nil
	} else {
		return notifier.messageProducer()
	}
}
