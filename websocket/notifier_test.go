// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package websocket

import (
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"log"
	"testing"
	"time"
)

func TestNotifier(t *testing.T) {
	notifier := NewNotifier("testMessageType", generateTestMessage)

	// Should do nothing when there are no listeners.
	notifier.Notify()
	notifier.NotifyWithMessage(12345)
	notifier.NotifyWithMessage(struct{}{})

	listener := notifier.listen()
	notifier.Notify()
	message := <-listener
	assert.Equal(t, "testMessageType", message.messageType)
	assert.Equal(t, "test message", message.messageBody)
	notifier.NotifyWithMessage(12345)
	assert.Equal(t, 12345, (<-listener).messageBody)

	// Should allow multiple messages without blocking.
	notifier.NotifyWithMessage("message1")
	notifier.NotifyWithMessage("message2")
	notifier.Notify()
	assert.Equal(t, "message1", (<-listener).messageBody)
	assert.Equal(t, "message2", (<-listener).messageBody)
	assert.Equal(t, "test message", (<-listener).messageBody)

	// Should stop sending messages and not block once the buffer is full.
	log.SetOutput(ioutil.Discard) // Silence noisy log output.
	for i := 0; i < 20; i++ {
		notifier.NotifyWithMessage(i)
	}
	var value messageEnvelope
	var lastValue any
	for lastValue == nil {
		select {
		case value = <-listener:
		default:
			lastValue = value.messageBody
			return
		}
	}
	notifier.NotifyWithMessage("next message")
	assert.True(t, lastValue.(int) < 10)
	assert.Equal(t, "next message", (<-listener).messageBody)
}

func TestNotifyMultipleListeners(t *testing.T) {
	notifier := NewNotifier("testMessageType2", nil)
	listeners := [50]chan messageEnvelope{}
	for i := 0; i < len(listeners); i++ {
		listeners[i] = notifier.listen()
	}

	notifier.Notify()
	notifier.NotifyWithMessage(12345)
	for listener := range notifier.listeners {
		assert.Equal(t, nil, (<-listener).messageBody)
		assert.Equal(t, 12345, (<-listener).messageBody)
	}

	// Should reap closed channels automatically.
	close(listeners[4])
	notifier.NotifyWithMessage("message1")
	assert.Equal(t, 49, len(notifier.listeners))
	for listener := range notifier.listeners {
		assert.Equal(t, "message1", (<-listener).messageBody)
	}
	close(listeners[16])
	close(listeners[21])
	close(listeners[49])
	notifier.NotifyWithMessage("message2")
	assert.Equal(t, 46, len(notifier.listeners))
	for listener := range notifier.listeners {
		assert.Equal(t, "message2", (<-listener).messageBody)
	}
}

func generateTestMessage() any {
	return "test message"
}

// A listener that is merely slow keeps its subscription. Its reader is mid-write every time
// the buffer happens to be full, and dropping on the first miss would unsubscribe healthy
// clients under load.
func TestNotifierKeepsBrieflyBlockedListener(t *testing.T) {
	notifier := NewNotifier("test", nil)
	clock := time.Now()
	notifier.now = func() time.Time { return clock }

	listener := notifier.listen()
	for i := 0; i < notifyBufferSize+3; i++ {
		notifier.Notify()
	}
	assert.Len(t, notifier.listeners, 1, "a full listener must not be dropped immediately")

	// Well inside the timeout, and then draining restores it.
	clock = clock.Add(listenerBlockedTimeout / 2)
	notifier.Notify()
	assert.Len(t, notifier.listeners, 1)

	<-listener
	notifier.Notify()
	assert.Zero(t, notifier.listeners[listener].blockedSince, "draining should clear the blocked spell")
}

// A client that has gone away -- a kiosk carried out of wifi range -- never drains, and
// every event would otherwise log another failure for the life of the process.
func TestNotifierDropsPersistentlyBlockedListener(t *testing.T) {
	notifier := NewNotifier("test", nil)
	clock := time.Now()
	notifier.now = func() time.Time { return clock }

	notifier.listen()
	for i := 0; i < notifyBufferSize+1; i++ {
		notifier.Notify()
	}
	assert.Len(t, notifier.listeners, 1)

	clock = clock.Add(listenerBlockedTimeout)
	notifier.Notify()
	assert.Empty(t, notifier.listeners, "a listener blocked past the timeout should be dropped")

	// Dropped, not closed: the reader still owns the channel and closes it on its way out.
	// Notifying again must not panic on a send to a closed channel or a missing entry.
	assert.NotPanics(t, func() { notifier.Notify() })
}
