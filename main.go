package main

//
// mysqlbackup: Allows to backup an mysql/mariadb instance on Windows or Linux. Exporting structre, data and users. Following a retention policy, allowing to keep a encrypted copy of the backups on a remote host.
//
// Donationware für CFI Kinderhilfe. Lizenz: MIT mit Namensnennung.
//
// Version: 1.3.7.94 (in version.go zu ändern)
//
// ChangeLog:
// 28.02.26	1.3.7	Fixed: some linter warnings
// 27.02.26	1.3.6	Fixed: refactored the ssh handling to separate package!
// 26.02.26	1.3.1	Fixed: remove critical logs and parametersand enforced ssh host key usage according to results of security audit
// 26.02.26	1.3.0	Fixed: checking of existing windows tasks, Feature: Overview of backups by classes and calculating average backup time
// 11.02.26	1.2.0	Feature: included an way to fully restore a database
// 09.02.26	1.1.5	Fixed: Quotes for task scheduler arguments corrected
// 09.02.26	1.1.4	Fixed structure to comply with prepreaBuild
//
import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/mysqlbackup/internal/logger"
	"github.com/janmz/mysqlbackup/internal/mysql"
	"github.com/janmz/mysqlbackup/internal/remote"
	"github.com/janmz/mysqlbackup/internal/restore"
	"github.com/janmz/mysqlbackup/internal/retention"
	"github.com/janmz/mysqlbackup/internal/run"
	"github.com/janmz/mysqlbackup/internal/schedule"
)

func main() {
	// No Chdir here: ConfigPath must see real cwd so "invoked dir" (e.g. ./mysqlbackup from Elisa/) is resolved correctly; we Chdir to config dir after path is chosen.

	configPath := flag.String("config", "", "Pfad zur JSON-Config (Standard: aktuelles Verz. oder Home)")
	doVerbose := flag.Bool("v", false, "detaillierte Ausgaben mit [DEBUG], inkl. Exec-Aufrufe und Ausgaben")
	doVerboseLong := flag.Bool("verbose", false, "")
	doInit := flag.Bool("init", false, "Jobs erstellen (Task Scheduler / systemd-Timer)")
	doCleanConfig := flag.Bool("cleanconfig", false, "Config-Datei mit Klartextpasswörtern schreiben")
	doRemove := flag.Bool("remove", false, "Jobs löschen")
	doStatus := flag.Bool("status", false, "Config prüfen, Backupdateien und Job-Einstellung anzeigen")
	doBackup := flag.Bool("backup", false, "Backup ausführen (wird von Jobs übergeben)")
	doRestore := flag.Bool("restore", false, "Restore aus letztem Backup oder letztem vor optionalem Datum YYYYMMDD")
	doRestoreFull := flag.Bool("restorefull", false, "Full-Restore: data->data.old, Instanz-backup nach data, dann Import (optional YYYYMMDD)")
	getFile := flag.String("getfile", "", "Datei von Remote laden (ZIP-Backup-Dateiname)")
	doSetupSSH := flag.Bool("setup-ssh", false, "SSH-Host-Key vom Server holen und in remote_ssh_host_key eintragen; nur Testverbindung, dann Ende")
	flag.Usage = printUsage
	flag.Parse()
	verbose := *doVerbose || *doVerboseLong

	invokedDir := invokedDirectory()
	path := config.ConfigPath(*configPath, invokedDir)
	// Arbeitsverzeichnis = Verzeichnis der gewählten Config, damit relative Pfade (backup_dir, log, …) konsistent sind
	if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			if configDir := filepath.Dir(abs); configDir != "" {
				_ = os.Chdir(configDir)
			}
		}
	}

	// Nur eine Aktion pro Aufruf; ohne oder mit ungültigem Flag: Übersicht ausgeben
	n := 0
	if *doInit {
		n++
	}
	if *doCleanConfig {
		n++
	}
	if *doRemove {
		n++
	}
	if *doStatus {
		n++
	}
	if *doBackup {
		n++
	}
	if *doRestore {
		n++
	}
	if *doRestoreFull {
		n++
	}
	if *getFile != "" {
		n++
	}
	if *doSetupSSH {
		n++
	}
	args := flag.Args()
	if len(args) > 1 {
		printStartupHeader(path)
		printUsage()
		fmt.Fprintln(os.Stderr, i18n.T("error.restore_too_many_args"))
		os.Exit(1)
	}
	dateArg := ""
	if len(args) == 1 {
		if !*doRestore && !*doRestoreFull {
			printStartupHeader(path)
			printUsage()
			fmt.Fprintln(os.Stderr, i18n.T("error.restoredate_requires_restore"))
			os.Exit(1)
		}
		dateArg = strings.TrimSpace(args[0])
	}
	if n == 0 {
		printStartupHeader(path)
		printUsage()
		os.Exit(0)
	}
	if n > 1 {
		printStartupHeader(path)
		printUsage()
		fmt.Fprintln(os.Stderr, i18n.T("error.one_flag"))
		os.Exit(1)
	}

	switch {
	case *doInit:
		runInit(path, verbose)
		return
	case *doCleanConfig:
		runCleanConfig(path, verbose)
		return
	case *doRemove:
		runRemove(path, verbose)
		return
	case *doStatus:
		runStatus(path, verbose)
		return
	case *doBackup:
		runBackup(path, verbose)
		return
	case *doRestore:
		runRestore(path, dateArg, false, verbose)
		return
	case *doRestoreFull:
		runRestore(path, dateArg, true, verbose)
		return
	case *getFile != "":
		runGetfile(path, *getFile, verbose)
		return
	case *doSetupSSH:
		runSetupSSH(path, verbose)
		return
	}
}

// invokedDirectory returns the directory of the path used to start the program (e.g. ./mysqlbackup -> Elisa/), or "" if started by name from PATH.
// So when running from a subdir via symlink, config is taken from that subdir, not from the resolved binary's directory.
func invokedDirectory() string {
	if len(os.Args) == 0 {
		return ""
	}
	arg0 := os.Args[0]
	if arg0 == "" {
		return ""
	}
	// Path component? (./mysqlbackup, subdir/mysqlbackup, /usr/local/bin/mysqlbackup)
	if !strings.Contains(arg0, "/") && !strings.Contains(arg0, string(filepath.Separator)) {
		return ""
	}
	abs, err := filepath.Abs(arg0)
	if err != nil {
		return ""
	}
	return filepath.Dir(abs)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "%s\n\n", i18n.T("usage.title"))
	fmt.Fprintf(os.Stderr, "%s\n\n", i18n.T("usage.usage"))
	fmt.Fprintf(os.Stderr, "%s\n", i18n.T("usage.one_action"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.config"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.config_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.verbose"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.verbose_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.init"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.init_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.cleanconfig"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.cleanconfig_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.remove"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.remove_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.status"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.status_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.backup"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.backup_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.restore"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.restore_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.restorefull"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.restorefull_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.getfile"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.getfile_desc"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.getfile_wildcards"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.setup_ssh"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.setup_ssh_desc"))
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.help"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.help_desc"))
}

func loadConfigAndLog(path string, verbose bool) (*config.Config, *logger.Logger, error) {
	cfg, err := config.Load(path, false)
	if err != nil {
		return nil, nil, err
	}
	logPath := cfg.LogFilename
	if logPath == "" {
		if exe, err := os.Executable(); err == nil {
			if exeDir := filepath.Dir(exe); exeDir != "" {
				logPath = filepath.Join(exeDir, "mysqlbackup.log")
			}
		}
		if logPath == "" {
			logPath = filepath.Join(cfg.BackupDir, "mysqlbackup.log")
		}
	}
	log, err := logger.New(logPath)
	if err != nil {
		return nil, nil, err
	}
	if absLog, err := filepath.Abs(logPath); err == nil {
		fmt.Fprintln(os.Stderr, i18n.Tf("section.log_file", absLog))
	}
	log.Verbose = verbose
	logStartup(log)
	for _, msg := range config.Validate(cfg) {
		log.WarnS(msg)
	}
	return cfg, log, nil
}

// logStartup schreibt Aufrufpfad, Versionsnummer und Aufrufparameter ins Log (beim Start).
func logStartup(log *logger.Logger) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	log.InfoS(i18n.Tf("log.start.executable", exe))
	log.InfoS(i18n.Tf("log.start.version", Version))
	log.InfoS(i18n.Tf("log.start.arguments", os.Args[1:]))
}

// getLogPath returns the log file path from config (same resolution as loadConfigAndLog).
func getLogPath(cfg *config.Config) string {
	logPath := cfg.LogFilename
	if logPath != "" {
		return filepath.FromSlash(filepath.Clean(logPath))
	}
	if exe, err := os.Executable(); err == nil {
		if exeDir := filepath.Dir(exe); exeDir != "" {
			return filepath.Join(exeDir, "mysqlbackup.log")
		}
	}
	return filepath.Join(cfg.BackupDir, "mysqlbackup.log")
}

// logLineTimestamp parses the RFC3339 timestamp from a log line ("2006-01-02T15:04:05Z07:00 [LEVEL] msg"). Returns zero time if not found.
func logLineTimestamp(line string) time.Time {
	idx := strings.Index(line, " [")
	if idx <= 0 {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(line[:idx]))
	return t
}

// isBackupStart returns true if the line is the backup run start (log.start.arguments with "[--backup ").
// Application and log are assumed to use the same language; uses i18n.T("log.start.arguments") prefix.
func isBackupStart(line string) bool {
	prefix := strings.Replace(i18n.T("log.start.arguments"), "%v", "", 1)
	if !strings.Contains(line, prefix) {
		return false
	}
	if !strings.Contains(line, " -backup ") && !strings.Contains(line, "[--backup ") {
		return false
	}
	if strings.Contains(line, " -restore") || strings.Contains(line, "--restore") {
		return false
	}
	return true
}

// isBackupOK returns true if the line is log.msg.backup_ok. Application and log same language; uses i18n.T().
func isBackupOK(line string) bool {
	return strings.Contains(line, i18n.T("log.msg.backup_ok"))
}

// lastBackupDurations reads the log file and returns the last n backup durations in seconds (chronological order).
// Start = log line with header.arguments containing "[--backup "; End = log line with log.msg.backup_ok.
func lastBackupDurations(logPath string, n int) ([]int, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var durations []int
	var startTime time.Time
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		t := logLineTimestamp(line)
		if isBackupStart(line) {
			startTime = t
		}
		if isBackupOK(line) && !startTime.IsZero() {
			durations = append(durations, int(t.Sub(startTime).Seconds()))
			startTime = time.Time{}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(durations) <= n {
		return durations, nil
	}
	return durations[len(durations)-n:], nil
}

// printStartupHeader schreibt denselben Header wie beim Backup (Version, Aufrufpfad, Parameter, Config-Pfad) auf stderr, damit bei jedem Aufruf die laufende Version sichtbar ist.
func printStartupHeader(configPath string) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	fmt.Fprintln(os.Stderr, i18n.Tf("header.version", Version))
	fmt.Fprintln(os.Stderr, i18n.Tf("header.executable", exe))
	fmt.Fprintln(os.Stderr, i18n.Tf("header.arguments", os.Args[1:]))
	if configPath != "" {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			absPath = configPath
		}
		fmt.Fprintln(os.Stderr, i18n.Tf("section.config_file", absPath))
	}
}

func runInit(path string, verbose bool) {
	printStartupHeader(path)
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()
	if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.init")+"\n", err)
		os.Exit(1)
	}
	fmt.Println(i18n.Tf("msg.jobs_created", path))
}

func runCleanConfig(path string, verbose bool) {
	printStartupHeader(path)
	if verbose {
		fmt.Fprintln(os.Stderr, i18n.T("log.debug.loadclean"))
	}
	if err := config.LoadClean(path, verbose); err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.cleanconfig")+"\n", err)
		os.Exit(1)
	}
	fmt.Println(i18n.Tf("msg.cleanconfig_done", path))
}

func runRemove(path string, verbose bool) {
	printStartupHeader(path)
	var log *logger.Logger
	if cfg, err := config.Load(path, false); err == nil {
		logPath := cfg.LogFilename
		if logPath == "" {
			logPath = filepath.Join(cfg.BackupDir, "mysqlbackup.log")
		}
		log, _ = logger.New(logPath)
	}
	if log == nil {
		log, _ = logger.New("mysqlbackup.log")
	}
	if log != nil {
		log.Verbose = verbose
		logStartup(log)
		defer log.Close()
	}
	if err := schedule.Uninstall(log); err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.remove")+"\n", err)
		os.Exit(1)
	}
	fmt.Println(i18n.T("msg.jobs_removed"))
}

func runStatus(path string, verbose bool) {
	printStartupHeader(path)
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
			log.WarnS(i18n.Tf("log.warn.schedule_ensure", err))
		}
	}
	fmt.Println(i18n.T("section.config"))
	fmt.Println(i18n.Tf("section.config_file", path))
	fmt.Println(i18n.Tf("section.mysql", cfg.MySQLHost, cfg.MySQLPort))
	fmt.Println(i18n.Tf("section.backup_dir", cfg.BackupDir))
	fmt.Println(i18n.Tf("section.retention", cfg.RetainDaily, cfg.RetainWeekly, cfg.RetainMonthly, cfg.RetainYearly))
	fmt.Println(i18n.Tf("section.start_time", cfg.StartTime))
	if cfg.RemoteBackupDir != "" && cfg.RemoteSSHHost != "" {
		fmt.Println(i18n.Tf("section.remote", cfg.RemoteBackupDir, cfg.RemoteSSHHost))
	}
	fmt.Println()
	fmt.Println(i18n.T("section.job"))
	if key, args := schedule.Status(cfg, path); key != "" {
		fmt.Println(i18n.Tf(key, args...))
	} else {
		fmt.Println(i18n.T("msg.no_job"))
	}
	fmt.Println()
	fmt.Println(i18n.T("section.backups"))
	files, err := retention.ListBackups(cfg.BackupDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.Tf("section.backup_dir_error", err))
	} else if len(files) == 0 {
		fmt.Println(i18n.T("msg.no_backups"))
	} else {
		const (
			wDate = 19 // 2006-01-02 15:04:05
			wSize = 6  // max 1023T
			wName = 60
			wKind = 12
		)
		var totalSize int64
		var yearly, monthly, weekly, daily int
		seen := make(map[string]bool)
		for _, f := range files {
			kind := retention.Classify(f.Date)
			totalSize += f.Size
			name := filepath.Base(f.Path)
			if len(name) > wName {
				name = name[:wName-1] + "…"
			}
			dayKey := f.Date.Format("20060102")
			if !seen[dayKey] { // attention: [] delivers the zero value (in this case false) if the key ist not found!
				seen[dayKey] = true
				switch retention.PeriodKey(f.Date) {
				case "yearly":
					yearly++
				case "monthly":
					monthly++
				case "weekly":
					weekly++
				default:
					daily++
				}
			}
			fmt.Printf("%-*s %*s %-*s %-*s\n",
				wDate, f.ModTime.Format("2006-01-02 15:04:05"),
				wSize, formatSize(f.Size),
				wName, name,
				wKind, "("+kind+")")
		}
		fmt.Printf("%-*s %*s %-*s\n",
			wDate, i18n.T("status.summe"),
			wSize, formatSize(totalSize),
			wName, i18n.Tf("msg.files_count", len(files)))
		fmt.Println()
		fmt.Println(i18n.Tf("status.retention_overview", yearly, monthly, weekly, daily))
		fmt.Println(i18n.T("status.retention_hint"))
	}
	printBackupDurations(cfg)
}

// formatDuration returns a human-readable duration (e.g. "2m 30s" or "45s").
func formatDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := sec / 60
	s := sec % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}

// printBackupDurations reads the log file for the last 10 backup durations and prints min, max, average.
func printBackupDurations(cfg *config.Config) {
	logPath := getLogPath(cfg)
	durations, err := lastBackupDurations(logPath, 10)
	if err != nil || len(durations) == 0 {
		return
	}
	minSec := durations[0]
	maxSec := durations[0]
	sum := 0
	for _, s := range durations {
		if s < minSec {
			minSec = s
		}
		if s > maxSec {
			maxSec = s
		}
		sum += s
	}
	avgSec := sum / len(durations)
	fmt.Println()
	fmt.Println(i18n.T("status.section_durations"))
	fmt.Println(i18n.Tf("status.duration_last_n", len(durations)))
	fmt.Println(i18n.Tf("status.duration_min", formatDuration(minSec)))
	fmt.Println(i18n.Tf("status.duration_max", formatDuration(maxSec)))
	fmt.Println(i18n.Tf("status.duration_avg", formatDuration(avgSec)))
}

// formatSize formats size: bytes without suffix; 1024*n as "nK", 1024²*n as "nM", 1024³*n as "nT"; one decimal if value < 10, else none.
func formatSize(n int64) string {
	const k = 1024
	if n < k {
		return strconv.FormatInt(n, 10)
	}
	if n < k*k {
		v := float64(n) / k
		if v < 10 {
			return fmt.Sprintf("%.1fK", v)
		}
		return fmt.Sprintf("%dK", int64(v))
	}
	if n < k*k*k {
		v := float64(n) / (k * k)
		if v < 10 {
			return fmt.Sprintf("%.1fM", v)
		}
		return fmt.Sprintf("%dM", int64(v))
	}
	v := float64(n) / (k * k * k)
	if v < 10 {
		return fmt.Sprintf("%.1fT", v)
	}
	return fmt.Sprintf("%dT", int64(v))
}

func runGetfile(path, filename string, verbose bool) {
	printStartupHeader(path)
	if !validGetfilePattern(filename) {
		fmt.Fprintln(os.Stderr, i18n.T("error.getfile_no_path"))
		os.Exit(1)
	}
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.workdir")+"\n", err)
		os.Exit(1)
	}
	saved, err := remote.GetFile(cfg, filename, cwd, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.getfile")+"\n", err)
		os.Exit(1)
	}
	for _, p := range saved {
		fmt.Println(i18n.Tf("msg.saved", p))
	}
}

func runSetupSSH(path string, verbose bool) {
	printStartupHeader(path)
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()
	if cfg.RemoteSSHHost == "" {
		log.ErrorS(i18n.T("err.remote_not_configured"))
		fmt.Fprintln(os.Stderr, i18n.T("err.remote_not_configured"))
		os.Exit(1)
	}
	keyLine, err := remote.FetchServerHostKey(cfg)
	if err != nil {
		log.WarnS(i18n.Tf("log.msg.setup_ssh_connection_failed", err))
		fmt.Fprintln(os.Stderr, i18n.Tf("log.msg.setup_ssh_connection_failed", err))
		os.Exit(1)
	}
	log.InfoS(i18n.T("log.msg.setup_ssh_connection_ok"))
	log.InfoS(i18n.T("log.msg.setup_ssh_key_found"))
	alreadyPresent, err := remote.HostKeyAlreadyPresent(cfg.RemoteSSHHostKey, keyLine)
	if err != nil {
		log.WarnS(i18n.Tf("log.msg.setup_ssh_update_failed", err))
		fmt.Fprintln(os.Stderr, i18n.Tf("log.msg.setup_ssh_update_failed", err))
		os.Exit(1)
	}
	if alreadyPresent {
		log.InfoS(i18n.T("log.msg.setup_ssh_key_already_present"))
		return
	}
	if err := config.UpdateRemoteSSHHostKey(cfg, path, keyLine); err != nil {
		log.WarnS(i18n.Tf("log.msg.setup_ssh_update_failed", err))
		fmt.Fprintln(os.Stderr, i18n.Tf("log.msg.setup_ssh_update_failed", err))
		os.Exit(1)
	}
	log.InfoS(i18n.T("log.msg.setup_ssh_updated"))
}

// validGetfilePattern ensures the argument has no path components (no /, \, ..).
func validGetfilePattern(s string) bool {
	if s == "" || filepath.Base(s) != s {
		return false
	}
	return !containsPath(s)
}

func containsPath(s string) bool {
	if len(s) >= 2 && s[0] == '.' && s[1] == '.' {
		return true
	}
	for _, r := range s {
		if r == '/' || r == '\\' {
			return true
		}
	}
	return false
}

func runBackup(path string, verbose bool) {
	printStartupHeader(path)
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()

	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		log.WarnS(i18n.T("log.warn.schedule_platform"))
	} else {
		if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
			log.WarnS(i18n.Tf("log.warn.schedule_ensure", err))
		}
	}

	if err := run.Backup(cfg, log); err != nil {
		log.ErrorS(i18n.Tf("log.error.backup_failed", err))
		os.Exit(1)
	}
	log.InfoS(i18n.T("log.msg.backup_ok"))
}

func runRestore(path, dateStr string, full bool, verbose bool) {
	printStartupHeader(path)
	cfg, log, err := loadConfigAndLog(path, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.config")+"\n", err)
		os.Exit(1)
	}
	defer log.Close()

	var beforeDate *time.Time
	if strings.TrimSpace(dateStr) != "" {
		t, err := time.ParseInLocation("20060102", strings.TrimSpace(dateStr), time.Local)
		if err != nil {
			fmt.Fprintf(os.Stderr, i18n.T("error.restoredate_format")+"\n", err)
			os.Exit(1)
		}
		beforeDate = &t
	}

	files, err := retention.LastBackupBefore(cfg.BackupDir, beforeDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.restore_select")+"\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, i18n.T("error.restore_no_backup_found"))
		os.Exit(1)
	}

	password := cfg.RootPassword
	if full {
		if err := restore.FullReinit(cfg, log); err != nil {
			fmt.Fprintf(os.Stderr, i18n.T("error.restorefull")+"\n", err)
			os.Exit(1)
		}
		password = ""
	}

	conn := &mysql.Conn{
		Host:     cfg.MySQLHost,
		Port:     cfg.MySQLPort,
		User:     "root",
		Password: password,
		BinDir:   cfg.MySQLBin,
	}
	if err := restore.RestoreFromZips(conn, files, log); err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("error.restore")+"\n", err)
		os.Exit(1)
	}
	log.InfoS(i18n.T("log.msg.restore_ok"))
}
