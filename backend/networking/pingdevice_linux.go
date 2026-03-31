//go:build linux

package networking

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
	probing "github.com/prometheus-community/pro-bing"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

func PingDevice(device *core.Record) (bool, error) {
	ping_cmd := device.GetString("ping_cmd")
	if ping_cmd == "" {
		pinger, err := probing.NewPinger(device.GetString("ip"))
		if err != nil {
			return false, err
		}
		pinger.Count = 1
		pinger.Timeout = 500 * time.Millisecond

		privileged := true
		privilegedEnv := os.Getenv("UPSNAP_PING_PRIVILEGED")
		if privilegedEnv != "" {
			privileged, err = strconv.ParseBool(privilegedEnv)
			if err != nil {
				privileged = false
			}
		}
		if privileged {
			orig := cap.GetProc()
			defer orig.SetProc() // restore original caps on exit.

			c, err := orig.Dup()
			if err != nil {
				return false, fmt.Errorf("Failed to dup existing capabilities: %v", err)
			}

			if on, _ := c.GetFlag(cap.Permitted, cap.NET_RAW); !on {
				return false, fmt.Errorf("Privileged ping selected but NET_RAW capability not permitted")
			}

			if err := c.SetFlag(cap.Effective, true, cap.NET_RAW); err != nil {
				return false, fmt.Errorf("unable to set NET_RAW capability")
			}

			if err := c.SetProc(); err != nil {
				return false, fmt.Errorf("unable to raise NET_RAW capability")
			}
		}
		pinger.SetPrivileged(privileged)

		err = pinger.Run()
		if err != nil {
			if isNoRouteOrDownError(err) {
				return false, nil
			}
			return false, err
		}
		stats := pinger.Statistics()
		return stats.PacketLoss == 0, nil
	} else {
		var shell string
		var shell_arg string

		shell = "/bin/sh"
		shell_arg = "-c"

		cmd := exec.Command(shell, shell_arg, ping_cmd)
		err := cmd.Run()

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode := exitErr.ExitCode()
				logger.Info.Printf("Custom ping for %s (id: %s) returned exit code %d. Treating as offline.", device.GetString("name"), device.Id, exitCode)
				return false, nil
			}
			return false, fmt.Errorf("custom ping command execution failed: %w", err)
		}
		return true, nil
	}
}
