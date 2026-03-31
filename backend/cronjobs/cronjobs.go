package cronjobs

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/robfig/cron/v3"
	"github.com/monstercjz/upsnap/logger"
	"github.com/monstercjz/upsnap/networking"
)

var (
	PingRunning         = false
	WakeShutdownRunning = false
	CronPing            = cron.New(cron.WithParser(cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
	CronWakeShutdown = cron.New(cron.WithParser(cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
)

func SetPingJobs(app *pocketbase.PocketBase) {
	// remove existing jobs
	for _, job := range CronPing.Entries() {
		CronPing.Remove(job.ID)
	}

	settingsPrivateRecords, err := app.FindAllRecords("settings_private")
	if err != nil {
		logger.Error.Println(err)
	}

	CronPing.AddFunc(settingsPrivateRecords[0].GetString("interval"), func() {
		// skip cron if no realtime clients connected and lazy_ping is turned on
		realtimeClients := len(app.SubscriptionsBroker().Clients())
		if realtimeClients == 0 && settingsPrivateRecords[0].GetBool("lazy_ping") {
			return
		}

		devices, err := app.FindAllRecords("devices")
		if err != nil {
			logger.Error.Println(err)
			return
		}

		// expand ports field
		expandFetchFunc := func(c *core.Collection, ids []string) ([]*core.Record, error) {
			return app.FindRecordsByIds(c.Id, ids, nil)
		}
		merr := app.ExpandRecords(devices, []string{"ports"}, expandFetchFunc)
		if len(merr) > 0 {
			return
		}

		for _, device := range devices {
			// ping device
			go func(d *core.Record) {
				status := d.GetString("status")
				if status == "pending" {
					return
				}
				isUp, err := networking.PingDevice(d)
				if err != nil {
					logger.Error.Println(err)
				}
				if isUp {
					if status == "online" {
						return
					}
					d.Set("status", "online")
					if err := app.Save(d); err != nil {
						logger.Error.Println("Failed to save record:", err)
					}
				} else {
					if status == "offline" {
						return
					}
					d.Set("status", "offline")
					if err := app.Save(d); err != nil {
						logger.Error.Println("Failed to save record:", err)
					}
				}
			}(device)

			// ping ports
			go func(d *core.Record) {
				ports, err := app.FindRecordsByIds("ports", d.GetStringSlice("ports"))
				if err != nil {
					logger.Error.Println(err)
				}
				for _, port := range ports {
					isUp, err := networking.CheckPort(d.GetString("ip"), port.GetString("number"))
					if err != nil {
						logger.Error.Println("Failed to check port:", err)
					}
					if isUp != port.GetBool("status") {
						port.Set("status", isUp)
						if err := app.Save(port); err != nil {
							logger.Error.Println("Failed to save record:", err)
						}
					}
				}
			}(device)
		}
	})
}

func SetWakeShutdownJobs(app *pocketbase.PocketBase) {
	// remove existing jobs
	entries := CronWakeShutdown.Entries()
	logger.Info.Printf("Resetting Wake/Shutdown jobs. Current entries: %d", len(entries))
	for _, job := range entries {
		CronWakeShutdown.Remove(job.ID)
	}

	devices, err := app.FindAllRecords("devices")
	if err != nil {
		logger.Error.Println(err)
		return
	}
	logger.Info.Printf("Found %d devices to process for cronjobs", len(devices))

	for _, dev := range devices {
		wake_cron := dev.GetString("wake_cron")
		wake_cron_enabled := dev.GetBool("wake_cron_enabled")
		shutdown_cron := dev.GetString("shutdown_cron")
		shutdown_cron_enabled := dev.GetBool("shutdown_cron_enabled")

		logger.Info.Printf("Device: %s (id: %s). WakeCron: '%s', Enabled: %t", dev.GetString("name"), dev.Id, wake_cron, wake_cron_enabled)

		if wake_cron_enabled && wake_cron != "" {
			deviceID := dev.Id
			deviceName := dev.GetString("name")
			_, err := CronWakeShutdown.AddFunc(wake_cron, func() {
				logger.Info.Printf("Cron fired: Wake for %s (id: %s)", deviceName, deviceID)
				d, err := app.FindRecordById("devices", deviceID)
				if err != nil {
					logger.Error.Println(err)
					return
				}
				if d.GetString("status") == "pending" {
					logger.Info.Printf("Device %s is already pending, skipping wake cron", deviceName)
					return
				}
				isOnline, err := networking.PingDevice(d)
				if err != nil {
					logger.Error.Println(err)
					return
				}
				if isOnline {
					logger.Info.Printf("Device %s is already online, skipping wake trigger", deviceName)
					return
				}
				logger.Info.Printf("Device %s is offline, proceeding to wake", deviceName)
				d.Set("status", "pending")
				if err := app.Save(d); err != nil {
					logger.Error.Println("Failed to save record:", err)
					return
				}
				if err := networking.WakeDevice(d); err != nil {
					logger.Error.Println(err)
					d.Set("status", "offline")
				} else {
					d.Set("status", "online")
				}
				if err := app.Save(d); err != nil {
					logger.Error.Println("Failed to save record:", err)
				}
			})
			if err != nil {
				logger.Error.Printf("Failed to add wake cron for %s: %+v", deviceName, err)
			} else {
				logger.Info.Printf("Successfully registered wake cron for %s", deviceName)
			}
		}

		if shutdown_cron_enabled && shutdown_cron != "" {
			deviceID := dev.Id
			deviceName := dev.GetString("name")
			_, err := CronWakeShutdown.AddFunc(shutdown_cron, func() {
				logger.Info.Printf("Cron fired: Shutdown for %s (id: %s)", deviceName, deviceID)
				d, err := app.FindRecordById("devices", deviceID)
				if err != nil {
					logger.Error.Println(err)
					return
				}
				if d.GetString("status") == "pending" {
					logger.Info.Printf("Device %s is already pending, skipping shutdown cron", deviceName)
					return
				}
				isOnline, err := networking.PingDevice(d)
				if err != nil {
					logger.Error.Println(err)
					return
				}
				if !isOnline {
					logger.Info.Printf("Device %s is already offline, skipping shutdown trigger", deviceName)
					return
				}
				status := d.GetString("status")
				if status != "online" {
					logger.Info.Printf("Device %s status is %s (not online), skipping shutdown trigger", deviceName, status)
					return
				}
				logger.Info.Printf("Device %s is online, proceeding to shutdown", deviceName)
				d.Set("status", "pending")
				if err := app.Save(d); err != nil {
					logger.Error.Println("Failed to save record:", err)
				}
				if err := networking.ShutdownDevice(d); err != nil {
					logger.Error.Println(err)
					d.Set("status", "online")
				} else {
					d.Set("status", "offline")
				}
				if err := app.Save(d); err != nil {
					logger.Error.Println("Failed to save record:", err)
				}
			})
			if err != nil {
				logger.Error.Printf("Failed to add shutdown cron for %s: %+v", deviceName, err)
			} else {
				logger.Info.Printf("Successfully registered shutdown cron for %s", deviceName)
			}
		}
	}
}

func StartWakeShutdown() {
	WakeShutdownRunning = true
	go CronWakeShutdown.Run()

}

func StopWakeShutdown() {
	if WakeShutdownRunning {
		logger.Info.Println("Stopping wake/shutdown cronjob")
		CronWakeShutdown.Stop()
	}
	WakeShutdownRunning = false
}

func StartPing() {
	PingRunning = true
	go CronPing.Run()
}

func StopPing() {
	if PingRunning {
		logger.Info.Println("Stopping wake/shutdown cronjob")
		CronPing.Stop()
	}
	PingRunning = false
}

func StopAll() {
	StopPing()
	StopWakeShutdown()
}
