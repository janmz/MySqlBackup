// Package restore implements restore operations from backup ZIP files.
package restore

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/mysqlbackup/internal/mysql"
	"github.com/janmz/mysqlbackup/internal/retention"
)

type Logger interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}

// RestoreFromZips imports SQL from each backup zip file in order.
func RestoreFromZips(conn *mysql.Conn, files []retention.BackupFile, log Logger) error {
	if len(files) == 0 {
		return fmt.Errorf(i18n.T("err.restore_no_backups"))
	}
	for _, f := range files {
		log.Info(i18n.Tf("log.msg.restore_zip", filepath.Base(f.Path)))
		if err := restoreZip(conn, f.Path); err != nil {
			return fmt.Errorf(i18n.Tf("err.restore_zip", filepath.Base(f.Path)), err)
		}
	}
	log.Info(i18n.Tf("log.msg.restore_done", len(files)))
	return nil
}

func restoreZip(conn *mysql.Conn, zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	var sqlFile *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Ext(f.Name), ".sql") {
			sqlFile = f
			break
		}
	}
	if sqlFile == nil {
		return fmt.Errorf(i18n.T("err.restore_sql_missing"), filepath.Base(zipPath))
	}

	in, err := sqlFile.Open()
	if err != nil {
		return err
	}
	defer in.Close()

	pr, pw := io.Pipe()
	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(pw, in)
		_ = pw.CloseWithError(err)
		copyErr <- err
	}()

	importErr := conn.ImportSQL(pr)
	_ = pr.Close()
	if err := <-copyErr; err != nil {
		return err
	}
	if importErr != nil {
		return importErr
	}
	return nil
}

// FullReinit replaces MySQL/MariaDB data directory with the instance backup template and starts the server.
func FullReinit(cfg *config.Config, log Logger) error {
	dataDir := strings.TrimSpace(cfg.MySQLDataDir)
	if dataDir == "" {
		return fmt.Errorf(i18n.T("err.restorefull_data_dir"))
	}
	backupDir := strings.TrimSpace(cfg.MySQLBackupDir)
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(dataDir), "backup")
	}
	dataDir = filepath.FromSlash(filepath.Clean(dataDir))
	backupDir = filepath.FromSlash(filepath.Clean(backupDir))
	dataOldDir := dataDir + ".old"

	if info, err := os.Stat(backupDir); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return fmt.Errorf(i18n.T("err.restorefull_backup_dir"), err)
	}
	if _, err := os.Stat(dataOldDir); err == nil {
		return fmt.Errorf(i18n.T("err.restorefull_data_old_exists"), dataOldDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf(i18n.T("err.restorefull_data_old_stat"), err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		return fmt.Errorf(i18n.T("err.restorefull_data_dir_missing"), err)
	}

	if portReachable(cfg.MySQLHost, cfg.MySQLPort) {
		if strings.TrimSpace(cfg.MySQLStopCmd) == "" {
			return fmt.Errorf(i18n.T("err.restorefull_stop_required"))
		}
		log.Info(i18n.Tf("log.msg.mysql_stopping", cfg.MySQLStopCmd))
		if err := runMySQLLifecycleCmd(cfg.MySQLStopCmd, log, true); err != nil {
			return fmt.Errorf(i18n.T("err.restorefull_stop"), err)
		}
		if !waitForPortState(cfg.MySQLHost, cfg.MySQLPort, false, 30*time.Second, 1*time.Second) {
			return fmt.Errorf(i18n.T("err.restorefull_stop_timeout"))
		}
	}

	log.Info(i18n.Tf("log.msg.restorefull_rename", dataDir, dataOldDir))
	if err := os.Rename(dataDir, dataOldDir); err != nil {
		return fmt.Errorf(i18n.T("err.restorefull_rename"), err)
	}
	log.Info(i18n.Tf("log.msg.restorefull_copy", backupDir, dataDir))
	if err := copyDir(backupDir, dataDir); err != nil {
		return fmt.Errorf(i18n.T("err.restorefull_copy"), err)
	}

	if strings.TrimSpace(cfg.MySQLStartCmd) == "" {
		return fmt.Errorf(i18n.T("err.restorefull_start_required"))
	}
	log.Info(i18n.Tf("log.msg.mysql_starting", cfg.MySQLStartCmd))
	if err := runMySQLLifecycleCmd(cfg.MySQLStartCmd, log, false); err != nil {
		return fmt.Errorf(i18n.T("err.restorefull_start"), err)
	}
	if !waitForPortState(cfg.MySQLHost, cfg.MySQLPort, true, 60*time.Second, 2*time.Second) {
		return fmt.Errorf(i18n.T("err.restorefull_start_timeout"))
	}
	return nil
}

func copyDir(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstDir, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()

		if d.IsDir() {
			return os.MkdirAll(target, mode.Perm())
		}
		if mode&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		return copyFile(path, target, mode.Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func runMySQLLifecycleCmd(command string, log Logger, waitForExit bool) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(command)
		if strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".cmd") {
			name = "cmd"
			args = []string{"/c", command}
		} else {
			parts := splitCommandLine(command)
			if len(parts) == 0 {
				return nil
			}
			name = parts[0]
			args = parts[1:]
		}
	} else {
		parts := splitCommandLine(command)
		if len(parts) == 0 {
			return nil
		}
		name = parts[0]
		args = parts[1:]
	}

	if !waitForExit {
		c := exec.Command(name, args...)
		c.Stdin = nil
		if devNull, err := os.Open(os.DevNull); err == nil {
			c.Stdout = devNull
			c.Stderr = devNull
			defer devNull.Close()
		}
		if err := c.Start(); err != nil {
			return fmt.Errorf(i18n.T("err.start_cmd"), err)
		}
		_ = c.Process.Release()
		log.Info(i18n.T("log.msg.mysql_start_background"))
		return nil
	}

	c := exec.Command(name, args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (output: %s)", err, string(out))
	}
	if len(out) > 0 {
		log.Info(i18n.Tf("log.msg.mysql_lifecycle", string(out)))
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

func waitForPortState(host string, port int, wantOpen bool, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		open := portReachable(host, port)
		if open == wantOpen {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

func portReachable(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
