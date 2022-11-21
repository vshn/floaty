package main

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

type FifoHandler struct {
	pipe   io.Reader
	events <-chan fsnotify.Event

	running map[string]context.CancelFunc

	handleNotification notificationHandlerFunc
}

type notificationHandlerFunc func(ctx context.Context, notification Notification)

func NewFifoHandler(cfg notifyConfig, pipe io.Reader, events <-chan fsnotify.Event) (*FifoHandler, error) {

	p, err := cfg.NewProvider()
	if err != nil {
		return nil, err
	}
	fh := &FifoHandler{
		pipe:               pipe,
		events:             events,
		running:            map[string]context.CancelFunc{},
		handleNotification: defaultNotificatonHandler(p, cfg),
	}
	return fh, nil
}

func (h FifoHandler) HandleFifo(ctx context.Context) error {

	for {
		select {
		case e := <-h.events:
			switch e.Op {
			case fsnotify.Write:
				s := bufio.NewScanner(h.pipe)
				for s.Scan() {
					line := s.Text()
					logrus.Infof("Got line: %q", s.Text())
					n, err := parseNotificationLine(line)
					if err != nil {
						logrus.Errorf("Failed to parse fifo event from keepalived, keepalived might be incompatible with the floaty version: %s", err)
						continue
					}
					err = h.handleNotifyEvent(ctx, n)
					if err != nil {
						logrus.Errorf("Failed to handle notify event: %s", err)
						continue
					}
					logrus.Infof("handled: %q", s.Text())
				}

			case fsnotify.Remove, fsnotify.Rename:
				return fmt.Errorf("Named pipe was removed. Quitting")
			}
		case <-ctx.Done():
			return nil
		}
		logrus.Infof("event done")
	}
}
func (h FifoHandler) handleNotifyEvent(ctx context.Context, n Notification) error {
	stopRunning, ok := h.running[n.Instance]
	if ok {
		stopRunning()
	}
	delete(h.running, n.Instance)
	runCtx, stop := context.WithCancel(ctx)
	h.running[n.Instance] = stop

	h.handleNotification(runCtx, n)
	return nil
}

func defaultNotificatonHandler(provider elasticIPProvider, cfg notifyConfig) notificationHandlerFunc {
	return func(ctx context.Context, notification Notification) {
		go func() {
			logrus.WithField("notification", notification).Infof("Handle Notification")
			err := handleNotification(ctx, provider, cfg, notification)
			if err != nil {
				logrus.Errorf("Failed to handle notification: %s", err)
			}
		}()
	}
}
