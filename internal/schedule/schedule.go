// Package schedule ensures a backup schedule is installed (Windows Task Scheduler or Linux systemd).
package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/logger"
)

const (
	taskNameWindows = "MySQLBackup"
	serviceName     = "mysqlbackup"
)

// EnsureInstalled checks if a schedule already exists; if not, installs it (Windows task or Linux systemd timer).
func EnsureInstalled(cfg *config.Config, configPath string, log *logger.Logger) error {
	if runtime.GOOS == "windows" {
		return ensureWindows(cfg, configPath, log)
	}
	return ensureLinux(cfg, configPath, log)
}

func ensureWindows(cfg *config.Config, configPath string, log *logger.Logger) error {
	// Check if task exists: schtasks /Query /TN "MySQLBackup"
	cmd := exec.Command("schtasks", "/Query", "/TN", taskNameWindows)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err == nil {
		log.Info("Windows task %s already exists", taskNameWindows)
		return nil
	}
	// Create task: daily at start_time, run current exe with -config
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exe = filepath.Clean(exe)
	workDir := filepath.Dir(configPath)
	if workDir == "" {
		workDir = "."
	}
	// start_time "22:00" -> /ST 22:00
	startTime := cfg.StartTime
	if startTime == "" {
		startTime = "22:00"
	}
	startTime = strings.TrimSpace(startTime)
	if len(startTime) == 5 && startTime[2] == ':' {
		// OK
	} else {
		startTime = "22:00"
	}
	// Task für aktuellen Benutzer (ohne /RU SYSTEM), damit kein Admin nötig ist
	args := []string{
		"/Create", "/F",
		"/TN", taskNameWindows,
		"/TR", fmt.Sprintf(`"%s" --backup -config "%s"`, exe, configPath),
		"/SC", "DAILY",
		"/ST", startTime,
	}
	cmd = exec.Command("schtasks", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks create: %w (output: %s)", err, string(out))
	}
	log.Info("Windows task %s created (daily at %s)", taskNameWindows, startTime)
	return nil
}

func ensureLinux(cfg *config.Config, configPath string, log *logger.Logger) error {
	// Check user systemd: ~/.config/systemd/user/mysqlbackup.timer
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	userDir := filepath.Join(home, ".config", "systemd", "user")
	timerPath := filepath.Join(userDir, serviceName+".timer")
	if _, err := os.Stat(timerPath); err == nil {
		log.Info("systemd timer %s already exists", timerPath)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exe = filepath.Clean(exe)
	startTime := cfg.StartTime
	if startTime == "" {
		startTime = "22:00"
	}
	// OnCalendar=*-*-* 22:00:00
	parts := strings.SplitN(startTime, ":", 2)
	hour, min := "22", "00"
	if len(parts) >= 2 {
		hour, min = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	onCalendar := fmt.Sprintf("*-*-* %s:%s:00", hour, min)

	serviceContent := fmt.Sprintf(`[Unit]
Description=MySQL Backup
After=network.target

[Service]
Type=oneshot
ExecStart=%s --backup -config %s
WorkingDirectory=%s

[Install]
WantedBy=default.target
`, exe, configPath, filepath.Dir(configPath))

	timerContent := fmt.Sprintf(`[Unit]
Description=Run MySQL Backup daily

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, onCalendar)

	if err := os.MkdirAll(userDir, 0755); err != nil {
		return fmt.Errorf("mkdir systemd user: %w", err)
	}
	servicePath := filepath.Join(userDir, serviceName+".service")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("write service: %w", err)
	}
	if err := os.WriteFile(timerPath, []byte(timerContent), 0644); err != nil {
		return fmt.Errorf("write timer: %w", err)
	}
	log.Info("systemd timer and service created in %s; run: systemctl --user daemon-reload && systemctl --user enable --now %s.timer", userDir, serviceName)
	return nil
}

// Status returns a short description of the current job (exists, next run, command). Empty if no job.
func Status(cfg *config.Config, configPath string) string {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("schtasks", "/Query", "/TN", taskNameWindows)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return ""
		}
		exe, _ := os.Executable()
		return "Windows Task: " + taskNameWindows + " (täglich um " + cfg.StartTime + ")\nBefehl: " + exe + " --backup -config " + configPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	timerPath := filepath.Join(home, ".config", "systemd", "user", serviceName+".timer")
	if _, err := os.Stat(timerPath); err != nil {
		return ""
	}
	return "systemd Timer: " + timerPath + " (täglich um " + cfg.StartTime + ")\nBefehl: " + serviceName + " --backup -config " + configPath
}

// Uninstall removes the scheduled task (Windows) or systemd timer (Linux). log may be nil.
func Uninstall(log *logger.Logger) error {
	info := func(format string, a ...interface{}) {
		if log != nil {
			log.Info(format, a...)
		}
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("schtasks", "/Delete", "/TN", taskNameWindows, "/F")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks delete: %w (output: %s)", err, string(out))
		}
		info("Windows task %s removed", taskNameWindows)
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	userDir := filepath.Join(home, ".config", "systemd", "user")
	timerPath := filepath.Join(userDir, serviceName+".timer")
	servicePath := filepath.Join(userDir, serviceName+".service")
	_ = os.Remove(timerPath)
	_ = os.Remove(servicePath)
	info("systemd timer and service files removed from %s; run: systemctl --user daemon-reload", userDir)
	return nil
}
