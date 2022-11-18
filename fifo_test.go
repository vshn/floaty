package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFIFO_integration(t *testing.T) {

	addr, err := parseNetAddress("10.10.1.1")
	require.NoError(t, err)
	refreshCounter := map[string]int{}
	provider := &fakeElasticIPProvider{refreshCounter: refreshCounter}
	cfg := notifyConfig{
		ManagedAddresses: []netAddress{addr},
		RefreshInterval:  100 * time.Millisecond,
		RefreshTimeout:   time.Second,
	}
	handler, pipe, eventChan := SetupFIFOTest(t, defaultNotificatonHandler(provider, cfg))

	ctx, done := context.WithCancel(context.Background())
	defer done()
	go func() {
		assert.NoError(t, handler.HandleFifo(ctx), "Handler should not fail")
	}()

	WriteToPipe(t, pipe, eventChan, "INSTANCE \"foo\" MASTER 100\n")
	require.Eventuallyf(t, func() bool {
		c, ok := refreshCounter[addr.String()]
		return ok && c > 0
	}, time.Second, 50*time.Millisecond, "Not updating IP as master")

	WriteToPipe(t, pipe, eventChan, "INSTANCE \"foo\" BACKUP 100\n")
	oldc := 0
	require.Eventually(t, func() bool {
		c, ok := refreshCounter[addr.String()]
		res := ok && c == oldc
		oldc = c
		return res
	}, 5*time.Second, 500*time.Millisecond, "Not stopping to update IP")
}

func TestFIFO_interleaving(t *testing.T) {
	nh := newFakeNotificationHandler()
	handler, pipe, eventChan := SetupFIFOTest(t, nh.GetHandler(t))
	ctx, done := context.WithCancel(context.Background())
	defer done()
	go func() {
		assert.NoError(t, handler.HandleFifo(ctx), "Handler should not fail")
	}()

	WriteToPipe(t, pipe, eventChan, "INSTANCE \"bar\" MASTER 100\n")
	WriteToPipe(t, pipe, eventChan, "INSTANCE \"foo\" MASTER 100\n")
	WriteToPipe(t, pipe, eventChan, "\n")
	WriteToPipe(t, pipe, eventChan, "INSTANCE \"bar\" BACKUP 100\n")
	nh.isEventuallyMaster(t, "foo")
	nh.isEventuallyNotMaster(t, "bar")

	WriteToPipe(t, pipe, eventChan, "INSTANCE \"foo\" FAULT 100\nINSTANCE \"bar\" FAULT 100\nINSTANCE \"bar\" MASTER 100")
	nh.isEventuallyNotMaster(t, "foo")
	nh.isEventuallyMaster(t, "bar")

	WriteToPipe(t, pipe, eventChan, "GROUP \"bar\" BACKUP 100\n")
	WriteToPipe(t, pipe, eventChan, "G s\"bar\" BACKUP 100\n")
	nh.isEventuallyNotMaster(t, "foo")
	nh.isEventuallyMaster(t, "bar")

	WriteToPipe(t, pipe, eventChan, "INSTANCE \"bar\" BACKUP 100\n")
	nh.isEventuallyNotMaster(t, "bar")
	WriteToPipe(t, pipe, eventChan, "INSTANCE \"foo\" MASTER 100\n")
	nh.isEventuallyMaster(t, "foo")
}

func SetupFIFOTest(t *testing.T, fn notificationHandlerFunc) (FifoHandler, *bytes.Buffer, chan fsnotify.Event) {
	var pipe bytes.Buffer
	eventChan := make(chan fsnotify.Event, 3)

	handler := FifoHandler{
		pipe:               &pipe,
		events:             eventChan,
		running:            map[string]context.CancelFunc{},
		handleNotification: fn,
	}

	return handler, &pipe, eventChan
}

func WriteToPipe(t *testing.T, pipe *bytes.Buffer, events chan fsnotify.Event, content string) {
	_, err := pipe.Write([]byte(content))
	require.NoError(t, err, "Failed to write to pipe, test is probably wrong")
	events <- fsnotify.Event{
		Op: fsnotify.Write,
	}
}

type fakeHandlerState struct {
	ctx    context.Context
	master bool
}

type fakeNotificationHandler struct {
	mu      sync.Mutex
	running map[string]fakeHandlerState
}

func newFakeNotificationHandler() *fakeNotificationHandler {
	return &fakeNotificationHandler{
		mu:      sync.Mutex{},
		running: map[string]fakeHandlerState{},
	}
}

func (h *fakeNotificationHandler) GetHandler(t *testing.T) notificationHandlerFunc {
	return func(ctx context.Context, notification Notification) {
		h.mu.Lock()
		defer h.mu.Unlock()
		oldCtx, ok := h.running[notification.Instance]
		if ok {
			assert.Error(t, oldCtx.ctx.Err(), "old handler not stopped")
		}
		h.running[notification.Instance] = fakeHandlerState{
			ctx:    ctx,
			master: notification.Status == NotificationMaster,
		}
	}
}

func (h *fakeNotificationHandler) isEventuallyMaster(t *testing.T, instance string) {
	require.Eventuallyf(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.running[instance]
		return ok && s.master
	}, time.Second, 10*time.Millisecond, "%s should be in master state", instance)
}
func (h *fakeNotificationHandler) isEventuallyNotMaster(t *testing.T, instance string) {
	require.Eventuallyf(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.running[instance]
		return !ok || !s.master
	}, time.Second, 10*time.Millisecond, "%s should be in master state", instance)
}
