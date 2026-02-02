// MySQL/MariaDB Backup – konfiguriert über janmz/sconfig (JSON).
// Donationware für CFI Kinderhilfe. Lizenz: MIT mit Namensnennung.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/logger"
	"github.com/janmz/mysqlbackup/internal/retention"
	"github.com/janmz/mysqlbackup/internal/run"
	"github.com/janmz/mysqlbackup/internal/schedule"
)

func main() {
	configPath := flag.String("config", "", "Pfad zur JSON-Config (Standard: aktuelles Verz. oder Home)")
	doInit := flag.Bool("init", false, "Jobs erstellen (Task Scheduler / systemd-Timer)")
	doCleanConfig := flag.Bool("cleanconfig", false, "Config-Datei mit Klartextpasswörtern schreiben")
	doRemove := flag.Bool("remove", false, "Jobs löschen")
	doStatus := flag.Bool("status", false, "Config prüfen, Backupdateien und Job-Einstellung anzeigen")
	doBackup := flag.Bool("backup", false, "Backup ausführen (wird von Jobs übergeben)")
	flag.Parse()

	path := config.ConfigPath(*configPath)

	// Nur eine Aktion pro Aufruf; ohne Flag = status
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
	if n == 0 {
		*doStatus = true
	}
	if n > 1 {
		fmt.Fprintln(os.Stderr, "Nur eines der Flags --init, --cleanconfig, --remove, --status, --backup angeben.")
		os.Exit(1)
	}

	switch {
	case *doInit:
		runInit(path, *configPath)
		return
	case *doCleanConfig:
		runCleanConfig(path)
		return
	case *doRemove:
		runRemove(path)
		return
	case *doStatus:
		runStatus(path)
		return
	case *doBackup:
		runBackup(path, *configPath)
		return
	}
}

func loadConfigAndLog(configPath, path string) (*config.Config, *logger.Logger, error) {
	cfg, err := config.Load(path, false)
	if err != nil {
		return nil, nil, err
	}
	logPath := cfg.LogFilename
	if logPath == "" {
		logPath = filepath.Join(cfg.BackupDir, "mysqlbackup.log")
	}
	log, err := logger.New(logPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, log, nil
}

func runInit(path, flagConfig string) {
	cfg, log, err := loadConfigAndLog(flagConfig, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()
	if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Jobs wurden erstellt. Nächtlicher Lauf: --backup -config", path)
}

func runCleanConfig(path string) {
	if err := config.LoadClean(path); err != nil {
		fmt.Fprintf(os.Stderr, "cleanconfig: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Config wurde mit Klartextpasswörtern geschrieben:", path)
}

func runRemove(path string) {
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
		defer log.Close()
	}
	if err := schedule.Uninstall(log); err != nil {
		fmt.Fprintf(os.Stderr, "remove: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Jobs wurden entfernt.")
}

func runStatus(path string) {
	cfg, err := config.Load(path, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("=== Config ===")
	fmt.Println("Config-Datei:", path)
	fmt.Println("MySQL:", cfg.MySQLHost, cfg.MySQLPort)
	fmt.Println("Backup-Verzeichnis:", cfg.BackupDir)
	fmt.Println("Retention: täglich", cfg.RetainDaily, "wöchentlich", cfg.RetainWeekly, "monatlich", cfg.RetainMonthly, "jährlich", cfg.RetainYearly)
	fmt.Println("Startzeit (Job):", cfg.StartTime)
	if cfg.RemoteBackupDir != "" && cfg.RemoteSSHHost != "" {
		fmt.Println("Remote:", cfg.RemoteBackupDir, "@", cfg.RemoteSSHHost)
	}
	fmt.Println()
	fmt.Println("=== Job ===")
	if job := schedule.Status(cfg, path); job != "" {
		fmt.Println(job)
	} else {
		fmt.Println("Kein Job eingerichtet. Nutzen Sie --init zum Anlegen.")
	}
	fmt.Println()
	fmt.Println("=== Backups (lokales Verzeichnis) ===")
	files, err := retention.ListBackups(cfg.BackupDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Backup-Verzeichnis: %v\n", err)
	} else if len(files) == 0 {
		fmt.Println("Keine Backupdateien gefunden.")
	} else {
		for _, f := range files {
			p := retention.Classify(f.Date)
			var kind string
			switch p {
			case retention.Daily:
				kind = "täglich"
			case retention.Weekly:
				kind = "wöchentlich"
			case retention.Monthly:
				kind = "monatlich"
			case retention.Yearly:
				kind = "jährlich"
			default:
				kind = "täglich"
			}
			fmt.Printf("%s  %s  (%s)\n", f.Date.Format("2006-01-02"), filepath.Base(f.Path), kind)
		}
	}
}

func runBackup(path, flagConfig string) {
	cfg, log, err := loadConfigAndLog(flagConfig, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		log.Warn("Automatische Job-Einrichtung nur unter Windows/Linux; --init ggf. manuell ausführen.")
	} else {
		if err := schedule.EnsureInstalled(cfg, path, log); err != nil {
			log.Warn("schedule ensure: %v", err)
		}
	}

	if err := run.Backup(cfg, log); err != nil {
		log.Error("backup failed: %v", err)
		os.Exit(1)
	}
	log.Info("backup completed successfully")
}
