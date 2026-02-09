// Package schedule ensures a backup schedule is installed (Windows Task Scheduler, Linux systemd, or cron fallback).
package schedule

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/mysqlbackup/internal/logger"
)

const (
	taskNameWindows   = "MySQLBackup"
	serviceName       = "mysqlbackup"
	cronMarker        = "mysqlbackup-schedule"
	systemCrontabUser = "root" // user for /etc/crontab line (format: min hour * * * user command)
)

// systemCrontabPaths: tried in order when crontab executable is not available (e.g. Synology).
var systemCrontabPaths = []string{"/etc/crontab", "/usr/etc/crontab"}

// runWithDebug runs cmd via CombinedOutput; when log.Verbose, logs command and output with [DEBUG].
func runWithDebug(log *logger.Logger, cmd *exec.Cmd) ([]byte, error) {
	if log != nil && log.Verbose {
		log.Debug("exec: %s %v", cmd.Path, cmd.Args)
	}
	out, err := cmd.CombinedOutput()
	if log != nil && log.Verbose {
		if len(out) > 0 {
			log.Debug("output: %s", string(out))
		}
		if err != nil {
			log.Debug("exit: %v", err)
		}
	}
	return out, err
}

// EnsureInstalled checks if a schedule exists and is up to date (paths match); if not or paths changed, (re)creates it.
// On Windows also applies WakeToRun, StartWhenAvailable, ExecutionTimeLimit 12h. Call from --backup and --status.
func EnsureInstalled(cfg *config.Config, configPath string, log *logger.Logger) error {
	if runtime.GOOS == "windows" {
		return ensureWindows(cfg, configPath, log)
	}
	return ensureUnix(cfg, configPath, log)
}

// windowsTaskGetRunString returns the current task's run string (Execute + Arguments) for comparison.
// Prefer PowerShell so we get the exact stored value; fallback to schtasks output.
func windowsTaskGetRunString(log *logger.Logger) (string, error) {
	// PowerShell returns the exact stored Execute and Arguments (no re-quoting)
	script := `$t = Get-ScheduledTask -TaskName '` + taskNameWindows + `' -ErrorAction SilentlyContinue; if ($t -and $t.Actions.Count -gt 0) { $a = $t.Actions[0]; $a.Execute + ' ' + $a.Arguments }`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := runWithDebug(log, cmd)
	if err == nil {
		s := strings.TrimSpace(string(out))
		if s != "" {
			return s, nil
		}
	}
	// Fallback: schtasks (may show different quoting)
	cmd = exec.Command("schtasks", "/Query", "/TN", taskNameWindows, "/FO", "LIST", "/V")
	out, err = runWithDebug(log, cmd)
	if err != nil {
		return "", err
	}
	const prefix = "Task To Run:"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
		}
	}
	return "", fmt.Errorf(i18n.T("err.task_cmd_not_found"))
}

// windowsTaskGetCommand returns the current task's exe and config path from schtasks /Query /FO LIST /V.
// Supports: "exe" --backup -config "config"; cmd /c cd /d "dir" && "exe" ... (new); cmd /c "cd /d \"dir\" && \"exe\" ..." (legacy).
func windowsTaskGetCommand(log *logger.Logger) (exe, configPath string, err error) {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskNameWindows, "/FO", "LIST", "/V")
	out, err := runWithDebug(log, cmd)
	if err != nil {
		return "", "", err
	}
	const prefix = "Task To Run:"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if len(rest) < 2 {
			continue
		}
		// Format 1a: cmd /c cd /d "workDir" or cmd.exe /c cd /d "workDir" (no outer quotes, plain " paths)
		afterCdPrefix := ""
		if strings.HasPrefix(rest, "cmd.exe /c cd /d ") {
			afterCdPrefix = "cmd.exe /c cd /d "
		} else if strings.HasPrefix(rest, "cmd /c cd /d ") {
			afterCdPrefix = "cmd /c cd /d "
		}
		if afterCdPrefix != "" && strings.Contains(rest, " && ") {
			afterCd := strings.TrimSpace(rest[len(afterCdPrefix):])
			_, rest2, ok := extractDoubleQuoted(afterCd)
			if ok {
				rest2 = strings.TrimSpace(rest2)
				if strings.HasPrefix(rest2, "&& ") {
					e, rest2, ok := extractDoubleQuoted(strings.TrimSpace(rest2[3:]))
					if ok {
						rest2 = strings.TrimSpace(rest2)
						if idx := strings.Index(rest2, "-config "); idx >= 0 {
							c, _, ok := extractDoubleQuoted(strings.TrimSpace(rest2[idx+8:]))
							if ok {
								return e, c, nil
							}
						}
					}
				}
			}
		}
		// Format 1b (legacy): cmd /c "cd /d \"workDir\" && \"exe\" --backup -config \"configPath\""
		if strings.HasPrefix(rest, "cmd ") && strings.Contains(rest, "cd /d ") && strings.Contains(rest, " && ") {
			quoted := extractBackslashQuotedPaths(rest)
			if len(quoted) >= 3 {
				return quoted[1], quoted[2], nil
			}
		}
		// Format 2: "exe" --backup -config "configPath"
		if rest[0] != '"' && rest[0] != '\'' {
			continue
		}
		quote := rest[0]
		end := 1
		for end < len(rest) {
			if rest[end] == quote && (end == 0 || rest[end-1] != '\\') {
				break
			}
			end++
		}
		if end >= len(rest) {
			continue
		}
		exe = rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
		if !strings.Contains(rest, "-config") {
			continue
		}
		idx := strings.Index(rest, "-config")
		rest = strings.TrimSpace(rest[idx+7:])
		if len(rest) < 2 || (rest[0] != '"' && rest[0] != '\'') {
			continue
		}
		quote2 := rest[0]
		end2 := 1
		for end2 < len(rest) {
			if rest[end2] == quote2 && (end2 == 0 || rest[end2-1] != '\\') {
				break
			}
			end2++
		}
		if end2 < len(rest) {
			configPath = rest[1:end2]
		}
		return exe, configPath, nil
	}
	return "", "", fmt.Errorf(i18n.T("err.task_cmd_not_found"))
}

// extractDoubleQuoted parses a double-quoted string at the start of s ("" inside is one "). Returns content, rest, ok.
func extractDoubleQuoted(s string) (content, rest string, ok bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	var buf strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				buf.WriteByte('"')
				i += 2
				continue
			}
			return buf.String(), s[i+1:], true
		}
		buf.WriteByte(s[i])
		i++
	}
	return "", "", false
}

// extractBackslashQuotedPaths returns strings between \" and \" (order: workDir, exe, configPath)
func extractBackslashQuotedPaths(s string) []string {
	var out []string
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '\\' || s[i+1] != '"' {
			continue
		}
		i += 2
		var buf strings.Builder
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) && s[i+1] == '"' {
				buf.WriteByte('"')
				i += 2
				continue
			}
			if s[i] == '"' {
				out = append(out, buf.String())
				i++
				break
			}
			buf.WriteByte(s[i])
			i++
		}
	}
	return out
}

func windowsPathsMatch(currentExe, currentConfig, taskExe, taskConfig string) bool {
	norm := func(s string) string {
		s = filepath.Clean(s)
		s = strings.ReplaceAll(s, "/", "\\")
		return strings.TrimSpace(s)
	}
	return strings.EqualFold(norm(currentExe), norm(taskExe)) && strings.EqualFold(norm(currentConfig), norm(taskConfig))
}

// resolveDriveToUNC converts a path like N:\folder to \\server\share\folder when N: is a mapped network drive.
// Returns the original path if not Windows, not a drive letter, or resolution fails.
func resolveDriveToUNC(path string, log *logger.Logger) string {
	if runtime.GOOS != "windows" || path == "" {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return path // already UNC
	}
	if len(path) < 2 || path[1] != ':' {
		return path
	}
	drive := path[:2] // e.g. "N:"
	if (drive[0] < 'A' || drive[0] > 'Z') && (drive[0] < 'a' || drive[0] > 'z') {
		return path
	}
	// PowerShell: get RemoteName for this drive (only set for network drives)
	script := fmt.Sprintf("try { (Get-Item -LiteralPath '%s').PSDrive.RemoteName } catch { '' }", drive+`\`)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := runWithDebug(log, cmd)
	if err != nil {
		return path
	}
	uncRoot := strings.TrimSpace(string(out))
	if uncRoot == "" || !strings.HasPrefix(uncRoot, `\\`) {
		return path
	}
	uncRoot = strings.TrimSuffix(uncRoot, `\`)
	rest := path[2:] // "\folder\file" or "folder\file"
	if len(rest) > 0 && rest[0] != '\\' {
		rest = `\` + rest
	}
	return uncRoot + rest
}

func applyWindowsTaskSettings(log *logger.Logger) {
	// Set-ScheduledTask -InputObject does not apply Settings; use -TaskName -Settings with New-ScheduledTaskSettingsSet.
	script := `$s = New-ScheduledTaskSettingsSet -WakeToRun -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Hours 12); Set-ScheduledTask -TaskName '` + taskNameWindows + `' -Settings $s`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if _, err := runWithDebug(log, cmd); err != nil {
		if log != nil {
			log.Warn(i18n.Tf("log.warn.powershell_settings", err))
		}
		return
	}
	if log != nil {
		log.Info(i18n.T("log.msg.windows_task_settings"))
	}
}

// applyWindowsTaskWorkingDir sets the task action's WorkingDirectory so relative log/backup paths resolve (e.g. on UNC shares).
func applyWindowsTaskWorkingDir(workDir string, log *logger.Logger) {
	// Escape single quotes for PowerShell: ' -> ''
	esc := escapeForPSSingleQuoted(workDir)
	script := `$t = Get-ScheduledTask -TaskName '` + taskNameWindows + `' -ErrorAction SilentlyContinue; if ($t) { $a = $t.Actions[0]; $a.WorkingDirectory = '` + esc + `'; Set-ScheduledTask -TaskName '` + taskNameWindows + `' -Action $a }`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if _, err := runWithDebug(log, cmd); err != nil {
		if log != nil {
			log.Warn(i18n.Tf("log.warn.powershell_workdir", err))
		}
		return
	}
	if log != nil {
		log.Info(i18n.T("log.msg.windows_task_workdir"))
	}
}

// escapeForPSSingleQuoted escapes a string for use inside a PowerShell single-quoted string (' -> '').
func escapeForPSSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// createWindowsTaskViaPowerShell creates the scheduled task via PowerShell so the exact command and WorkingDirectory are stored (no schtasks re-quoting).
func createWindowsTaskViaPowerShell(taskName, cmdArgument, workingDir, startTime string, log *logger.Logger) error {
	argEsc := escapeForPSSingleQuoted(cmdArgument)
	wdEsc := escapeForPSSingleQuoted(workingDir)
	// WorkingDirectory must be in quotes in the script when path has spaces; pass as single-quoted so it is stored literally including the path
	script := `$arg = '` + argEsc + `'; $wd = '` + wdEsc + `'; ` +
		`$a = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument $arg -WorkingDirectory $wd; ` +
		`$t = New-ScheduledTaskTrigger -Daily -At '` + startTime + `'; ` +
		`$s = New-ScheduledTaskSettingsSet -WakeToRun -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Hours 12); ` +
		`Register-ScheduledTask -TaskName '` + taskName + `' -Action $a -Trigger $t -Settings $s -Force`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := runWithDebug(log, cmd)
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func ensureWindows(cfg *config.Config, configPath string, log *logger.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf(i18n.T("err.executable_path"), err)
	}
	exe = filepath.Clean(exe)
	configPath = filepath.Clean(configPath)
	workDir := filepath.Dir(configPath)
	if workDir == "" {
		workDir = "."
	}
	// Prefer UNC paths for the task so it works when drive letters differ or are missing for the task user
	exeTask := resolveDriveToUNC(exe, log)
	configPathTask := resolveDriveToUNC(configPath, log)
	workDirTask := resolveDriveToUNC(workDir, log)

	startTime := cfg.StartTime
	if startTime == "" {
		startTime = "22:00"
	}
	startTime = strings.TrimSpace(startTime)
	if len(startTime) != 5 || startTime[2] != ':' {
		startTime = "22:00"
	}

	// Build the exact command we store: "cmd.exe /c cd /d "workDir" && "exe" --backup -config "configPath"" (paths with " escaped as "")
	pathForTR := func(s string) string { return strings.ReplaceAll(s, `"`, `""`) }
	cmdArgument := fmt.Sprintf(`/c cd /d "%s" && "%s" --backup -config "%s"`, pathForTR(workDirTask), pathForTR(exeTask), pathForTR(configPathTask))
	plannedTaskRun := "cmd.exe " + cmdArgument

	// If task exists, compare run string; only recreate when it differs (prevents losing task history)
	cmd := exec.Command("schtasks", "/Query", "/TN", taskNameWindows)
	_, errQuery := runWithDebug(log, cmd)
	taskExists := errQuery == nil
	if taskExists {
		existingRun, errGet := windowsTaskGetRunString(log)
		if errGet == nil && strings.TrimSpace(existingRun) == strings.TrimSpace(plannedTaskRun) {
			applyWindowsTaskSettings(log)
			applyWindowsTaskWorkingDir(workDirTask, log)
			log.Info(i18n.Tf("log.msg.windows_task_uptodate", taskNameWindows))
			return nil
		}
		if errGet == nil {
			log.Info(i18n.T("log.msg.windows_task_updating"))
		}
		// Delete so we can recreate with correct command
		del := exec.Command("schtasks", "/Delete", "/TN", taskNameWindows, "/F")
		_, _ = runWithDebug(log, del)
	}

	// Create via PowerShell so the exact Argument and WorkingDirectory are stored (no outer quotes, no backslash-escaping)
	if err := createWindowsTaskViaPowerShell(taskNameWindows, cmdArgument, workDirTask, startTime, log); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("err.schtasks_create"), err)
	}
	log.Info(i18n.Tf("log.msg.windows_task_created", taskNameWindows, startTime))
	applyWindowsTaskSettings(log)
	applyWindowsTaskWorkingDir(workDirTask, log)
	return nil
}

// ensureUnix tries systemd user timer first; if not available (e.g. no user session), falls back to cron.
func ensureUnix(cfg *config.Config, configPath string, log *logger.Logger) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(i18n.T("err.home_dir"), err)
	}
	userDir := filepath.Join(home, ".config", "systemd", "user")
	timerPath := filepath.Join(userDir, serviceName+".timer")
	if _, err := os.Stat(timerPath); err == nil {
		log.Info(i18n.Tf("log.msg.systemd_exists", timerPath))
		return nil
	}
	if systemdUserAvailable(log) {
		return ensureLinuxSystemd(cfg, configPath, log)
	}
	log.Warn(i18n.T("log.warn.systemd_fallback"))
	return ensureUnixCron(cfg, configPath, log)
}

// systemdUserAvailable returns true if systemctl --user can be used (user session present).
func systemdUserAvailable(log *logger.Logger) bool {
	cmd := exec.Command("systemctl", "--user", "list-timers", "--no-legend")
	_, err := runWithDebug(log, cmd)
	if err != nil {
		return false
	}
	return true
}

func ensureLinuxSystemd(cfg *config.Config, configPath string, log *logger.Logger) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(i18n.T("err.home_dir"), err)
	}
	userDir := filepath.Join(home, ".config", "systemd", "user")
	timerPath := filepath.Join(userDir, serviceName+".timer")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf(i18n.T("err.executable_path"), err)
	}
	exe = filepath.Clean(exe)
	startTime := cfg.StartTime
	if startTime == "" {
		startTime = "22:00"
	}
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
		return fmt.Errorf(i18n.T("err.mkdir_systemd_user"), err)
	}
	servicePath := filepath.Join(userDir, serviceName+".service")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf(i18n.T("err.write_service"), err)
	}
	if err := os.WriteFile(timerPath, []byte(timerContent), 0644); err != nil {
		return fmt.Errorf(i18n.T("err.write_timer"), err)
	}
	log.Info(i18n.Tf("log.msg.systemd_created", userDir, serviceName))
	return nil
}

// quoteForCron returns s quoted for use in a crontab command line so that spaces and special characters are preserved.
func quoteForCron(s string) string {
	// Single-quote the whole string; escape any single quote as '\''
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

// ensureUnixCron adds a crontab entry for the current user (fallback when systemd user is not available).
func ensureUnixCron(cfg *config.Config, configPath string, log *logger.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf(i18n.T("err.executable_path"), err)
	}
	exe = filepath.Clean(exe)
	hour, min := 22, 0
	if t := strings.TrimSpace(cfg.StartTime); t != "" {
		parts := strings.SplitN(t, ":", 2)
		if len(parts) >= 2 {
			if h, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && h >= 0 && h <= 23 {
				hour = h
			}
			if m, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && m >= 0 && m <= 59 {
				min = m
			}
		}
	}
	exeQ := quoteForCron(exe)
	configQ := quoteForCron(configPath)
	cronLineUser := fmt.Sprintf("%d %d * * * %s --backup -config %s # %s", min, hour, exeQ, configQ, cronMarker)
	cronLineSystem := fmt.Sprintf("%d %d * * * %s %s --backup -config %s # %s", min, hour, systemCrontabUser, exeQ, configQ, cronMarker)
	existing, err := getCrontab()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ensureUnixCronSystemFile(hour, min, exe, cronLineSystem, log)
		}
		return fmt.Errorf(i18n.T("err.crontab_l"), err)
	}
	var newCrontab bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(existing))
	foundMarker := false
	replacedOrAppended := false
	for scanner.Scan() {
		line := scanner.Bytes()
		lineStr := strings.TrimSpace(string(line))
		if strings.Contains(lineStr, cronMarker) {
			foundMarker = true
			if lineStr == cronLineUser {
				log.Info(i18n.T("log.msg.cron_present"))
				return nil
			}
			// Replace first matching line; skip any further lines that also contain the marker
			if !replacedOrAppended {
				newCrontab.WriteString(cronLineUser)
				newCrontab.WriteByte('\n')
				replacedOrAppended = true
			}
			continue
		}
		newCrontab.Write(line)
		newCrontab.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf(i18n.T("err.crontab_l"), err)
	}
	if !foundMarker {
		newCrontab.WriteString(cronLineUser)
		newCrontab.WriteByte('\n')
		replacedOrAppended = true
	}
	if !replacedOrAppended {
		return nil
	}
	if err := setCrontab(newCrontab.Bytes()); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ensureUnixCronSystemFile(hour, min, exe, cronLineSystem, log)
		}
		return fmt.Errorf(i18n.T("err.crontab"), err)
	}
	log.Info(i18n.Tf("log.msg.cron_added", hour, min))
	return nil
}

// ensureUnixCronSystemFile appends the cron line to /etc/crontab (or /usr/etc/crontab) when crontab executable is not available.
func ensureUnixCronSystemFile(hour, min int, exe, cronLine string, log *logger.Logger) error {
	var path string
	var data []byte
	var err error
	for _, p := range systemCrontabPaths {
		data, err = os.ReadFile(p)
		if err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return fmt.Errorf(i18n.T("err.crontab_manual"), err, cronLine)
	}
	var newContent bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	foundMarker := false
	replacedOrAppended := false
	for scanner.Scan() {
		line := scanner.Bytes()
		lineStr := strings.TrimSpace(string(line))
		if strings.Contains(lineStr, cronMarker) {
			foundMarker = true
			if lineStr == cronLine {
				log.Info(i18n.Tf("log.msg.cron_present_file", path))
				return nil
			}
			if !replacedOrAppended {
				newContent.WriteString(cronLine)
				newContent.WriteByte('\n')
				replacedOrAppended = true
			}
			continue
		}
		newContent.Write(line)
		newContent.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf(i18n.T("err.crontab_manual"), err, cronLine)
	}
	if !foundMarker {
		newContent.WriteString(cronLine)
		newContent.WriteByte('\n')
		replacedOrAppended = true
	}
	if !replacedOrAppended {
		return nil
	}
	if err := os.WriteFile(path, newContent.Bytes(), 0644); err != nil {
		return fmt.Errorf(i18n.Tf("err.write_cron_need_root", path), err, cronLine)
	}
	log.Info(i18n.Tf("log.msg.cron_added_file", path, hour, min))
	return nil
}

func getCrontab() ([]byte, error) {
	cmd := exec.Command("crontab", "-l")
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

func setCrontab(data []byte) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = nil
	return cmd.Run()
}

// Status returns a translation key and args for the current job (exists, next run, command). Empty key if no job.
func Status(cfg *config.Config, configPath string) (key string, args []interface{}) {
	startTime := cfg.StartTime
	if startTime == "" {
		startTime = "22:00"
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("schtasks", "/Query", "/TN", taskNameWindows)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return "", nil
		}
		exe, _ := os.Executable()
		return "job.windows", []interface{}{taskNameWindows, startTime, exe, configPath}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	timerPath := filepath.Join(home, ".config", "systemd", "user", serviceName+".timer")
	if _, err := os.Stat(timerPath); err == nil {
		return "job.systemd", []interface{}{timerPath, startTime, serviceName, configPath}
	}
	if crontabHasMarker() {
		exe, _ := os.Executable()
		return "job.cron", []interface{}{startTime, exe, configPath}
	}
	return "", nil
}

func crontabHasMarker() bool {
	data, err := getCrontab()
	if err == nil && bytes.Contains(data, []byte(cronMarker)) {
		return true
	}
	return systemCrontabHasMarker()
}

func systemCrontabHasMarker() bool {
	for _, p := range systemCrontabPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte(cronMarker)) {
			return true
		}
	}
	return false
}

func removeCrontabMarker() error {
	data, err := getCrontab()
	if err == nil && bytes.Contains(data, []byte(cronMarker)) {
		var out bytes.Buffer
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			line := sc.Bytes()
			if bytes.Contains(line, []byte(cronMarker)) {
				continue
			}
			out.Write(line)
			out.WriteByte('\n')
		}
		if err := sc.Err(); err != nil {
			return err
		}
		if err := setCrontab(out.Bytes()); err != nil {
			return err
		}
	}
	return removeSystemCrontabMarker()
}

func removeSystemCrontabMarker() error {
	for _, path := range systemCrontabPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !bytes.Contains(data, []byte(cronMarker)) {
			continue
		}
		var out bytes.Buffer
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			line := sc.Bytes()
			if bytes.Contains(line, []byte(cronMarker)) {
				continue
			}
			out.Write(line)
			out.WriteByte('\n')
		}
		if err := sc.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(path, out.Bytes(), 0644); err != nil {
			return fmt.Errorf(i18n.Tf("err.write_path", path), err)
		}
		return nil
	}
	return nil
}

// Uninstall removes the scheduled task (Windows), systemd timer (Linux), or cron entry. log may be nil.
func Uninstall(log *logger.Logger) error {
	info := func(format string, a ...interface{}) {
		if log != nil {
			log.Info(format, a...)
		}
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("schtasks", "/Delete", "/TN", taskNameWindows, "/F")
		out, err := runWithDebug(log, cmd)
		if err != nil {
			return fmt.Errorf(i18n.T("err.schtasks_delete"), err, string(out))
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
	hadSystemd := false
	if _, err := os.Stat(timerPath); err == nil {
		hadSystemd = true
	}
	_ = os.Remove(timerPath)
	_ = os.Remove(servicePath)
	if hadSystemd {
		info("systemd timer and service files removed from %s; run: systemctl --user daemon-reload", userDir)
	}
	if crontabHasMarker() {
		if err := removeCrontabMarker(); err != nil {
			return fmt.Errorf(i18n.T("err.remove_cron"), err)
		}
		info("cron entry for mysqlbackup removed")
	}
	return nil
}
