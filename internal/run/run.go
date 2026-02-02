// Package run orchestrates backup: disk check, mysql, backup, retention, remote, email on error.
package run

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/backup"
	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/disk"
	"github.com/janmz/mysqlbackup/internal/email"
	"github.com/janmz/mysqlbackup/internal/logger"
	"github.com/janmz/mysqlbackup/internal/mysql"
	"github.com/janmz/mysqlbackup/internal/remote"
	"github.com/janmz/mysqlbackup/internal/retention"
)

// Backup runs the full backup flow: disk check, ensure schedule, list DBs, export users, parse, dump+append+zip, retention, remote copy. On critical error sends email and returns error.
func Backup(cfg *config.Config, log *logger.Logger) error {
	backupDir := filepath.FromSlash(cfg.BackupDir)
	avail, err := disk.Available(backupDir)
	if err != nil {
		log.Warn("disk available check: %v", err)
	} else if avail < disk.MinFreeBytes {
		err := fmt.Errorf("insufficient disk space: %d bytes available, need at least %d", avail, disk.MinFreeBytes)
		sendErrorEmail(cfg, log, "MySQL Backup: Speicherplatz zu gering", err.Error(), nil)
		return err
	}

	conn := &mysql.Conn{
		Host:     cfg.MySQLHost,
		Port:     cfg.MySQLPort,
		User:     "root",
		Password: cfg.RootPassword,
		BinDir:   cfg.MySQLBin,
	}

	weStartedMySQL := false
	if cfg.MySQLAutoStartStop && cfg.MySQLStartCmd != "" && cfg.MySQLStopCmd != "" {
		if err := conn.Reachable(); err != nil {
			// Fallback: Wenn Port 3306 offen ist, läuft MySQL evtl. schon (z. B. mysql-CLI nicht im PATH).
			// Dann nicht starten (Port schon belegt → Start würde fehlschlagen).
			if portReachable(conn.Host, conn.Port) {
				log.Info("MySQL-Port %s:%d offen, überspringe Start (mysql-CLI evtl. nicht im PATH?)", conn.Host, conn.Port)
			} else {
				log.Info("MySQL nicht erreichbar, starte mit: %s", cfg.MySQLStartCmd)
				if err := runMySQLLifecycleCmd(cfg.MySQLStartCmd, log); err != nil {
					sendErrorEmail(cfg, log, "MySQL Backup: MySQL-Start fehlgeschlagen", err.Error(), nil)
					return fmt.Errorf("mysql start: %w", err)
				}
				if !waitForMySQL(conn, 60*time.Second, 2*time.Second) {
					sendErrorEmail(cfg, log, "MySQL Backup: MySQL nach Start nicht erreichbar", "Timeout beim Warten auf MySQL", nil)
					return fmt.Errorf("mysql not reachable after start (timeout)")
				}
				weStartedMySQL = true
				log.Info("MySQL wurde gestartet")
			}
		}
	}

	isMariaDB, err := conn.IsMariaDB()
	if err != nil {
		sendErrorEmail(cfg, log, "MySQL Backup: Server nicht erreichbar", err.Error(), nil)
		return fmt.Errorf("mysql server: %w", err)
	}

	dbs, err := conn.ListDatabases()
	if err != nil {
		sendErrorEmail(cfg, log, "MySQL Backup: Datenbanken auflisten fehlgeschlagen", err.Error(), nil)
		return fmt.Errorf("list databases: %w", err)
	}
	if len(dbs) == 0 {
		log.Info("no user databases to backup")
		return nil
	}

	userSQL, err := conn.ExportUsers(isMariaDB)
	if err != nil {
		// Fallback for MySQL without mysqlpump: skip user export, only dump DBs
		log.Warn("export users failed (mysqlpump/mysqldump --system=users): %v; continuing without user grants in dumps", err)
		userSQL = []byte{}
	}

	createdFiles, err := backup.Run(cfg, conn, userSQL, dbs, isMariaDB, log)
	if err != nil {
		sendErrorEmail(cfg, log, "MySQL Backup: Dump fehlgeschlagen", err.Error(), nil)
		return fmt.Errorf("backup: %w", err)
	}

	if err := retention.ApplyToDirs(cfg.BackupDir, cfg.RemoteBackupDir, cfg.RetainDaily, cfg.RetainWeekly, cfg.RetainMonthly, cfg.RetainYearly, log); err != nil {
		log.Warn("retention: %v", err)
	}

	if err := remote.Copy(cfg, createdFiles, log); err != nil {
		sendErrorEmail(cfg, log, "MySQL Backup: Remote-Kopie fehlgeschlagen", err.Error(), nil)
		return fmt.Errorf("remote copy: %w", err)
	}

	if weStartedMySQL && cfg.MySQLAutoStartStop && cfg.MySQLStopCmd != "" {
		log.Info("Stoppe MySQL (war von uns gestartet): %s", cfg.MySQLStopCmd)
		if err := runMySQLLifecycleCmd(cfg.MySQLStopCmd, log); err != nil {
			log.Warn("MySQL-Stop: %v", err)
		}
	}

	return nil
}

// runMySQLLifecycleCmd runs a start/stop command. On Windows, .bat/.cmd are run via cmd /c.
func runMySQLLifecycleCmd(cmd string, log *logger.Logger) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(cmd)
		if strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".cmd") {
			name = "cmd"
			args = []string{"/c", cmd}
		} else {
			parts := splitCommandLine(cmd)
			if len(parts) == 0 {
				return nil
			}
			name = parts[0]
			args = parts[1:]
		}
	} else {
		parts := splitCommandLine(cmd)
		if len(parts) == 0 {
			return nil
		}
		name = parts[0]
		args = parts[1:]
	}
	// Timeout, falls Batch hängt (z. B. wartet auf Eingabe)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	// Stdin von NUL/DevNull, damit XAMPP-Batch ("Drücken Sie eine beliebige Taste") nicht blockiert
	if f, err := os.Open(os.DevNull); err == nil {
		c.Stdin = f
		defer f.Close()
	} else {
		c.Stdin = nil
	}
	out, err := c.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout (Batch hängt?): %w (output: %s)", err, string(out))
		}
		msg := string(out)
		if strings.Contains(strings.ToLower(msg), "could not be started") || strings.Contains(msg, "konnte nicht gestartet") {
			msg += "\nHinweis: XAMPP/MySQL-Logs prüfen (z. B. mysql\\data\\*.err), Port 3306, my.ini."
		}
		return fmt.Errorf("%w (output: %s)", err, msg)
	}
	if len(out) > 0 {
		log.Info("mysql lifecycle: %s", string(out))
	}
	return nil
}

func splitCommandLine(s string) []string {
	var parts []string
	var b strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"', '\'':
			inQuote = !inQuote
		case ' ', '\t':
			if !inQuote && b.Len() > 0 {
				parts = append(parts, b.String())
				b.Reset()
			} else if inQuote {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

func waitForMySQL(conn *mysql.Conn, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn.Reachable() == nil {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

// portReachable returns true if host:port accepts a TCP connection (z. B. MySQL läuft, aber mysql-CLI fehlt im PATH).
func portReachable(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func sendErrorEmail(cfg *config.Config, log *logger.Logger, subject, errDetail string, logExcerpt []byte) {
	var excerpt string
	if len(logExcerpt) > 0 {
		excerpt = string(logExcerpt)
		if len(excerpt) > 4096 {
			excerpt = excerpt[len(excerpt)-4096:]
		}
	}
	body := email.FormatErrorBody(subject, errDetail, excerpt)
	if err := email.Send(cfg, subject, body); err != nil {
		log.Warn("sending error email: %v", err)
	}
}

// CaptureLogExcerpt reads the last N bytes from log file for error emails (optional).
func CaptureLogExcerpt(logPath string, maxBytes int) []byte {
	if logPath == "" || maxBytes <= 0 {
		return nil
	}
	f, err := os.Open(filepath.FromSlash(logPath))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	if size <= int64(maxBytes) {
		b := make([]byte, size)
		_, _ = f.Read(b)
		return b
	}
	b := make([]byte, maxBytes)
	_, _ = f.ReadAt(b, size-int64(maxBytes))
	return b
}
