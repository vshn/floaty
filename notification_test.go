package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNotification(t *testing.T) {
	parseTest := func(in []string, expectedInstance, expectedState string, fail bool) func(t *testing.T) {
		return func(t *testing.T) {
			n, err := parseNotification(in)
			if fail {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equalf(t, expectedInstance, n.Instance, "parsed instance did not match expectation")
			assert.Equalf(t, NotificationStatus(expectedState), n.Status, "parsed state did not match expectation")
		}
	}

	t.Run("master", parseTest([]string{"INSTANCE", "foo", "MASTER", "100"},
		"foo", "MASTER", false))
	t.Run("fault", parseTest([]string{"INSTANCE", "bar", "FAULT", "100"},
		"bar", "FAULT", false))
	t.Run("backup", parseTest([]string{"INSTANCE", "buzz", "BACKUP", "100"},
		"buzz", "BACKUP", false))

	t.Run("with space", parseTest([]string{"INSTANCE", "foo bar", "MASTER", "100"},
		"foo bar", "MASTER", false))

	t.Run("unexpected", parseTest([]string{"This", "is", "definitely", "not", "a", "notification"},
		"", "", true))
	t.Run("good length", parseTest([]string{"Still", "not", "a", "notification"},
		"", "", true))
	t.Run("unsported group", parseTest([]string{"GROUP", "foos", "MASTER", "100"},
		"", "", true))
}
