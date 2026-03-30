//go:build !linux

package networking

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
	probing "github.com/prometheus-community/pro-bing"
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

		privileged := true // default to privileged ping, required by Windows
		if runtime.GOOS == "darwin" {
			// macOS pings will fail when using privileged ping as non root, but works with non-privileged.
			privileged = false
		}
		privilegedEnv := os.Getenv("UPSNAP_PING_PRIVILEGED")
		if privilegedEnv != "" {
			privileged, err = strconv.ParseBool(privilegedEnv)
			if err != nil {
				privileged = false
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
		if runtime.GOOS == "windows" {
			shell = "cmd"
			shell_arg = "/C"
		} else {
			shell = "/bin/sh"
			shell_arg = "-c"
		}

		cmd := exec.Command(shell, shell_arg, ping_cmd)
		err := cmd.Run()

		if err != nil {
			// 检查是否是进程退出错误
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode := exitErr.ExitCode()
				// 退出码 1 视为正常的"设备离线"状态，不返回 error
				// 这与内置 ICMP ping 的行为一致（isNoRouteOrDownError 返回 false, nil）
				if exitCode == 1 {
					return false, nil
				}
				// 其他非零退出码（如 127 命令不存在）视为真正错误
				return false, fmt.Errorf("custom ping command failed with exit code %d: %w", exitCode, err)
			}
			// 系统级错误（如无法启动进程）
			return false, fmt.Errorf("custom ping command execution failed: %w", err)
		}
		return true, nil
	}
}
