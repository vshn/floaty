//go:build e2e
// +build e2e

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"
)

func TestE2E_Master(t *testing.T) {
	conf, cleanup, err := setupConfig(t.Name(), "192.168.1.1/32")
	require.NoErrorf(t, err, "failed to setup test env")
	defer cleanup()

	cmd := exec.Command("./floaty", conf, "INSTANCE", t.Name(), "MASTER", "100")
	out, stop, err := startCmd(cmd)
	require.NoError(t, err)
	defer stop()

	expectUpdate(t, out, "192.168.1.1/32", 3)
}

func TestE2E_MasterThenBackup(t *testing.T) {
	conf, cleanup, err := setupConfig(t.Name(), "192.168.1.2/32")
	require.NoErrorf(t, err, "failed to setup test env")
	defer cleanup()

	masterCmd := exec.Command("./floaty", conf, "INSTANCE", t.Name(), "MASTER", "100")
	mout, done, err := runCmd(masterCmd)
	expectUpdate(t, mout, "192.168.1.2/32", 1)

	backupCmd := exec.Command("./floaty", conf, "INSTANCE", t.Name(), "BACKUP", "100")
	out, err := backupCmd.CombinedOutput()
	assert.NoErrorf(t, err, "failed to run backup command:\n%s", string(out))

	assert.NoErrorf(t, done(), "failed to stop master command")
}

func TestE2E_MasterThenFault(t *testing.T) {
	conf, cleanup, err := setupConfig(t.Name(), "192.168.1.3/32")
	require.NoErrorf(t, err, "failed to setup test env")
	defer cleanup()

	masterCmd := exec.Command("./floaty", conf, "INSTANCE", t.Name(), "MASTER", "100")
	mout, done, err := runCmd(masterCmd)
	expectUpdate(t, mout, "192.168.1.3/32", 1)

	backupCmd := exec.Command("./floaty", conf, "INSTANCE", t.Name(), "FAULT", "100")
	out, err := backupCmd.CombinedOutput()
	assert.NoErrorf(t, err, "failed to run fault command:\n%s", string(out))

	assert.NoErrorf(t, done(), "failed to stop master command")
}

func startCmd(cmd *exec.Cmd) (*bytes.Buffer, func() error, error) {
	out := &bytes.Buffer{}
	cmd.Stdout = out
	err := cmd.Start()
	if err != nil {
		return nil, nil, err
	}
	return out, cmd.Process.Kill, nil
}

func runCmd(cmd *exec.Cmd) (*bytes.Buffer, func() error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	out := &bytes.Buffer{}
	cmd.Stdout = out
	doneC := make(chan error, 1)
	go func() {
		doneC <- cmd.Run()
	}()
	done := func() error {
		for {
			select {
			case err := <-doneC:
				cancel()
				return err
			case <-ctx.Done():
				cancel()
				return errors.New("Unable to stop command, timeout")
			default:
			}
		}
	}
	return out, done, nil
}

func expectUpdate(t *testing.T, buf *bytes.Buffer, addr string, n int) {
	count := 0
	for count < n {
		var line string
		var err error
		line, err = buf.ReadString('\n')
		for errors.Is(err, io.EOF) {
			line, err = buf.ReadString('\n')
		}
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("REFRESH %s\n", addr), line)
		count++
	}
	assert.Equalf(t, count, n, "Expected at least %d refreshes", n)
}

func setupConfig(name, addr string) (string, func() error, error) {
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error {
		return os.RemoveAll(dir)
	}

	kd := filepath.Join(dir, "keepalived.conf")
	kdConf := fmt.Sprintf(`
vrrp_instance %s {
  state MASTER
  interface eth0
  virtual_router_id 5
  priority 200
  advert_int 1
  virtual_ipaddress {
    %s dev eth0
  }
  track_script {
    chk_myscript
  }
}
  `, name, addr)
	if err := os.WriteFile(kd, []byte(kdConf), 0666); err != nil {
		return "", cleanup, err
	}

	conf := notifyConfig{
		LockFileTemplate:     filepath.Join(dir, "floaty.%s.lock"),
		KeepalivedConfigFile: kd,
		RefreshInterval:      time.Second,
		Provider:             "fake",
	}
	confF := filepath.Join(dir, "conf.yml")
	data, err := yaml.Marshal(conf)
	if err != nil {
		return "", cleanup, err
	}
	if err := os.WriteFile(confF, data, 0666); err != nil {
		return "", cleanup, err
	}
	return confF, cleanup, nil
}
