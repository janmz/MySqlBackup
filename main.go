package main

//
// mysqlbackup: Allows to backup an mysql/mariadb instance on Windows or Linux. Exporting structre, data and users. Following a retention policy, allowing to keep a encrypted copy of the backups on a remote host.
//
// Donationware für CFI Kinderhilfe. Lizenz: MIT mit Namensnennung.
//
// Version: 1.1.5.62 (in version.go zu ändern)
//
// ChangeLog:
// 09.02.26	1.1.5	Fixed: Quotes for task scheduler arguments corrected
// 09.02.26	1.1.4	Fixed structure to comply with prepreaBuild
//
import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/mysqlbackup/internal/logger"
	"github.com/janmz/mysqlbackup/internal/remote"
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
	getFile := flag.String("getfile", "", "Datei von Remote laden (ZIP-Backup-Dateiname)")
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
	if *getFile != "" {
		n++
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
	case *getFile != "":
		runGetfile(path, *getFile, verbose)
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
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.T("usage.getfile"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.getfile_desc"))
	fmt.Fprintf(os.Stderr, "      %s\n", i18n.T("usage.getfile_wildcards"))
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
	return cfg, log, nil
}

// logStartup schreibt Aufrufpfad, Versionsnummer und Aufrufparameter ins Log (beim Start).
func logStartup(log *logger.Logger) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	log.Info(i18n.Tf("log.start.executable", exe))
	log.Info(i18n.Tf("log.start.version", Version))
	log.Info(i18n.Tf("log.start.arguments", os.Args[1:]))
}

// printStartupHeader schreibt denselben Header wie beim Backup (Version, Aufrufpfad, Parameter, Config-Pfad) auf stderr, damit bei jedem Aufruf die laufende Version sichtbar ist.
func printStartupHeader(configPath string) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	fmt.Fprintf(os.Stderr, i18n.Tf("header.version", Version)+"\n")
	fmt.Fprintf(os.Stderr, i18n.Tf("header.executable", exe)+"\n")
	fmt.Fprintf(os.Stderr, i18n.Tf("header.arguments", os.Args[1:])+"\n")
	if configPath != "" {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			absPath = configPath
		}
		fmt.Fprintf(os.Stderr, i18n.Tf("section.config_file", absPath)+"\n")
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
			log.Warn(i18n.Tf("log.warn.schedule_ensure", err))
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
		fmt.Fprintf(os.Stderr, i18n.Tf("section.backup_dir_error", err)+"\n")
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
		for _, f := range files {
			kind := retention.Classify(f.Date)
			totalSize += f.Size
			name := filepath.Base(f.Path)
			if len(name) > wName {
				name = name[:wName-1] + "…"
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
	}
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
		log.Warn(i18n.T("log.warn.schedule_platform"))
	} else {
		if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
			log.Warn(i18n.Tf("log.warn.schedule_ensure", err))
		}
	}

	if err := run.Backup(cfg, log); err != nil {
		log.Error(i18n.Tf("log.error.backup_failed", err))
		os.Exit(1)
	}
	log.Info(i18n.T("log.msg.backup_ok"))
}
