package networking

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/monstercjz/upsnap/logger"
)

func WakeDevice(device *core.Record) error {
	logger.Info.Println("Wake triggered for", device.GetString("name"))

	wakeTimeout := device.GetInt("wake_timeout")
	if wakeTimeout <= 0 {
		wakeTimeout = 120
	}

	wake_cmd := device.GetString("wake_cmd")
	if wake_cmd != "" {
		var shell string
		var shell_arg string
		if runtime.GOOS == "windows" {
			shell = "cmd"
			shell_arg = "/C"
		} else {
			shell = "/bin/sh"
			shell_arg = "-c"
		}

		wake_cmd = strings.ReplaceAll(wake_cmd, "{{ DEVICE_IP }}", device.GetString("ip"))
		wake_cmd = strings.ReplaceAll(wake_cmd, "{{ DEVICE_MAC }}", device.GetString("mac"))

		ctx := context.Background()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		cmd := exec.CommandContext(ctx, shell, shell_arg, wake_cmd)
		SetProcessAttributes(cmd)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		logger.Info.Printf("Executing wake_cmd for %s (timeout: %d s): %s", device.GetString("name"), wakeTimeout, wake_cmd)
		if err := cmd.Start(); err != nil {
			logger.Error.Printf("Failed to start wake_cmd for %s: %v", device.GetString("name"), err)
			return err
		}

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		start := time.Now()

		for {
			select {
			case <-time.After(1 * time.Second):
				if time.Since(start) >= time.Duration(wakeTimeout)*time.Second {
					if cmd.Process != nil {
						if err := KillProcess(cmd.Process); err != nil {
							logger.Error.Println(err)
						}
					}
					return fmt.Errorf("%s not online after %d seconds", device.GetString("name"), wakeTimeout)
				}
				isOnline, err := PingDevice(device)
				if err != nil {
					logger.Error.Println(err)
					return err
				}
				if isOnline {
					if cmd.Process != nil {
						if err := KillProcess(cmd.Process); err != nil {
							// Process might have already finished
						}
					}
					logger.Info.Printf("Device %s came online after wake_cmd", device.GetString("name"))
					return nil
				}
			case err := <-done:
				if err != nil {
					logger.Error.Printf("wake_cmd for %s failed: %v. Stderr: %s", device.GetString("name"), err, stderr.String())
					return fmt.Errorf("wake_cmd failed: %s", stderr.String())
				}
				logger.Info.Printf("wake_cmd for %s finished (waiting for device to come online...)", device.GetString("name"))
				// Command finished successfully, but we continue the loop until device is online or timeout
			}
		}
	} else {
		err := SendMagicPacket(device)
		if err != nil {
			return err
		}

		start := time.Now()
		for {
			time.Sleep(1 * time.Second)
			isOnline, err := PingDevice(device)
			if err != nil {
				logger.Error.Println(err)
				return err
			}
			if isOnline {
				return nil
			}
			if time.Since(start) >= time.Duration(wakeTimeout)*time.Second {
				break
			}
		}
		return fmt.Errorf("%s not online after %d seconds", device.GetString("name"), wakeTimeout)
	}
}
