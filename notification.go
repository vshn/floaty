package main

import (
	"context"
	"encoding/csv"
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

func parseNotificationLine(line string) (Notification, error) {
	// Yes, this can actually be parsed as a CSV file with spaces as separators and it handles quoted string the same way a shell does.
	r := csv.NewReader(strings.NewReader(line))
	r.Comma = ' '
	notifications, err := r.ReadAll()
	if err != nil {
		return Notification{}, err
	}

	if len(notifications) != 1 {
		return Notification{}, fmt.Errorf("Failed to parse notification: %q", line)
	}

	return parseNotification(notifications[0])
}

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
