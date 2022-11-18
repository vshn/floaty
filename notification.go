package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

type Notification struct {
	Type     string
	Instance string
	Status   NotificationStatus
}

type NotificationStatus string

const (
	NotificationMaster NotificationStatus = "MASTER"
	NotificationFault  NotificationStatus = "FAULT"
	NotificationBackup NotificationStatus = "BACKUP"
)

func parseNotification(fields []string) (Notification, error) {
	line := strings.Join(fields, " ")
	if len(fields) != 4 {
		return Notification{}, fmt.Errorf("Notify message %q has an unexpected format", line)
	}
	if fields[0] == "GROUP" {
		return Notification{}, errors.New("Only instance notifications are supported")
	}
	if fields[0] != "INSTANCE" {
		return Notification{}, fmt.Errorf("Notify message %q has an unexpected format", line)
	}
	if !validVRRPStatus(fields[2]) {
		return Notification{}, fmt.Errorf("Notify message %q has an unexpected status", line)
	}
	return Notification{
		Type:     fields[0],
		Instance: fields[1],
		Status:   NotificationStatus(fields[2]),
	}, nil
}

func validVRRPStatus(status string) bool {
	switch NotificationStatus(status) {
	case NotificationMaster, NotificationFault, NotificationBackup:
		return true
	default:
		return false
	}
}

func handleNotification(ctx context.Context, provider elasticIPProvider, cfg notifyConfig, notification Notification) error {
	addresses, err := cfg.getAddresses(notification.Instance)
	if err != nil {
		return err
	}
	logrus.WithField("addresses", addresses).Infof("IP addresses")

	if notification.Status == NotificationMaster {
		logrus.WithField("updating elastic IP", addresses).Infof("IP addresses")
		return pinElasticIPs(ctx, provider, addresses, cfg)
	}
	return nil
}
