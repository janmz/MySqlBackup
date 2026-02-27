// Package remote copies backup files to a remote host via SFTP using
// github.com/janmz/sshcommands. Wrappers read config and call the library.
package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/sshcommands"
)

var backupZipRe = regexp.MustCompile(`^mysql_backup_\d{8}_.*\.zip$`)

func optsFromConfig(cfg *config.Config) *sshcommands.Opts {
	port := cfg.RemoteSSHPort
	if port <= 0 {
		port = 22
	}
	timeout := time.Duration(cfg.RemoteSSHTimeoutSeconds) * time.Second
	return &sshcommands.Opts{
		Host:     cfg.RemoteSSHHost,
		Port:     port,
		User:     cfg.RemoteSSHUser,
		KeyFile:  filepath.FromSlash(cfg.RemoteSSHKeyFile),
		Password: cfg.RemoteSSHPassword,
		HostKey:  strings.TrimSpace(cfg.RemoteSSHHostKey),
		Timeout:  timeout,
	}
}

func listLocalBackups(dir string) ([]sshcommands.LocalFile, error) {
	dir = filepath.FromSlash(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []sshcommands.LocalFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".zip" || !backupZipRe.MatchString(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		list = append(list, sshcommands.LocalFile{
			Name:    name,
			Path:    path,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}
	return list, nil
}

// Sync lists local backup zips and remote files; uploads local if missing or newer
// (optional AES-256); deletes remote files that are no longer present locally.
func Sync(cfg *config.Config, backupDir string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Error(string, ...interface{})
}) error {
	if cfg.RemoteBackupDir == "" || cfg.RemoteSSHHost == "" {
		return nil
	}
	localList, err := listLocalBackups(backupDir)
	if err != nil {
		return fmt.Errorf(i18n.T("err.list_local"), err)
	}
	opts := optsFromConfig(cfg)
	remoteDir := filepath.ToSlash(cfg.RemoteBackupDir)
	aesPassword := strings.TrimSpace(cfg.RemoteAESPassword)
	if err := sshcommands.Sync(opts, localList, remoteDir, aesPassword, log); err != nil {
		return fmt.Errorf(i18n.T("err.remote_sync"), err)
	}
	return nil
}

// GetFile downloads one or more backup files from the remote server into destDir.
// Pattern may be a literal filename or wildcards (*, ?). No path components.
// Returns the list of local paths where files were saved.
func GetFile(cfg *config.Config, pattern, destDir string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) ([]string, error) {
	if cfg.RemoteBackupDir == "" || cfg.RemoteSSHHost == "" {
		return nil, fmt.Errorf("%s", i18n.T("err.remote_not_configured"))
	}
	if !validGetfilePattern(pattern) {
		return nil, fmt.Errorf("%s", i18n.T("err.getfile_no_path"))
	}
	opts := optsFromConfig(cfg)
	remoteDir := filepath.ToSlash(cfg.RemoteBackupDir)
	aesPassword := strings.TrimSpace(cfg.RemoteAESPassword)
	saved, err := sshcommands.Download(opts, pattern, destDir, remoteDir, aesPassword, log)
	if err != nil {
		return nil, fmt.Errorf(i18n.Tf("err.file_failed", pattern), err)
	}
	return saved, nil
}

func validGetfilePattern(pattern string) bool {
	if pattern == "" || strings.Contains(pattern, "..") {
		return false
	}
	return filepath.Base(pattern) == pattern &&
		!strings.Contains(pattern, "/") && !strings.Contains(pattern, "\\")
}

// FetchServerHostKey connects to the SSH server without host key verification,
// captures the server's host key, and returns it as a single line.
func FetchServerHostKey(cfg *config.Config) (string, error) {
	opts := optsFromConfig(cfg)
	keyLine, err := sshcommands.FetchServerHostKey(opts)
	if err != nil {
		return "", err
	}
	return keyLine, nil
}

// HostKeyAlreadyPresent returns true if newKeyLine is already among the keys
// in currentValue (inline " || "-separated or path to a file).
func HostKeyAlreadyPresent(currentValue, newKeyLine string) (bool, error) {
	return sshcommands.HostKeyAlreadyPresent(currentValue, newKeyLine)
}
