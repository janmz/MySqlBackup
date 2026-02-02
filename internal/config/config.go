// Package config loads and holds MySQL backup configuration via janmz/sconfig (JSON).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janmz/sconfig"
)

// Config holds all settings for MySQL backup (JSON with sconfig secure password pairs).
type Config struct {
	Version int `json:"version"`

	MySQLHost     string `json:"mysql_host"`
	MySQLHostname string `json:"mysql_hostname"` // optional: für Benennung (Backup-Dateien), wenn mysql_host = localhost
	MySQLPort     int    `json:"mysql_port"`
	MySQLBin      string `json:"mysql_bin"`      // optional: Verzeichnis mit mysql, mysqldump, mysqlpump (z. B. D:\xampp\mysql\bin)

	// MySQL-Lifecycle (z. B. XAMPP): bei Backup prüfen, ob MySQL läuft; wenn nicht, starten, nach Backup wieder stoppen.
	MySQLAutoStartStop bool   `json:"mysql_auto_start_stop"`
	MySQLStartCmd      string `json:"mysql_start_cmd"`
	MySQLStopCmd       string `json:"mysql_stop_cmd"`

	RootPassword       string `json:"root_password"`
	RootSecurePassword string `json:"root_secure_password"`

	RetainDaily   int `json:"retain_daily"`
	RetainWeekly  int `json:"retain_weekly"`
	RetainMonthly int `json:"retain_monthly"`
	RetainYearly  int `json:"retain_yearly"`

	BackupDir   string `json:"backup_dir"`
	LogFilename string `json:"log_filename"`

	AdminEmail       string `json:"admin_email"`
	AdminSMTPServer  string `json:"admin_smtp_server"`
	AdminSMTPPort    int    `json:"admin_smtp_port"`
	AdminSMTPUser    string `json:"admin_smtp_user"`    // optional: Login (wenn leer = admin_email)
	AdminSMTPTLS     string `json:"admin_smtp_tls"`     // "tls" (implizit, Port 465), "starttls" (Port 587), "" = Auto
	AdminSMTPPassword       string `json:"admin_smtp_password"`
	AdminSMTPSecurePassword  string `json:"admin_smtp_secure_password"`

	RemoteBackupDir  string `json:"remote_backup_dir"`
	RemoteSSHHost    string `json:"remote_ssh_host"`
	RemoteSSHPort    int    `json:"remote_ssh_port"`
	RemoteSSHUser    string `json:"remote_ssh_user"`
	RemoteSSHPassword       string `json:"remote_ssh_password"`
	RemoteSSHSecurePassword string `json:"remote_ssh_secure_password"`
	RemoteSSHKeyFile string `json:"remote_ssh_key_file"`

	StartTime string `json:"start_time"`
}

// DefaultConfig returns config with default values.
func DefaultConfig() *Config {
	return &Config{
		MySQLPort:      3306,
		RetainDaily:   14,
		RetainWeekly:  3,
		RetainMonthly: 3,
		RetainYearly:  3,
		AdminSMTPPort: 587,
		RemoteSSHPort: 22,
		StartTime:     "22:00",
	}
}

// Load reads config from path via sconfig (JSON + secure passwords), then normalizes paths.
// If cleanConfig is true, sconfig writes the file back with plaintext passwords (for migration/inspection).
func Load(path string, cleanConfig bool) (*Config, error) {
	cfg := DefaultConfig()
	if err := sconfig.LoadConfig(cfg, cfg.Version, path, cleanConfig, false); err != nil {
		return nil, fmt.Errorf("sconfig load: %w", err)
	}
	cfg.normalizePaths()
	return cfg, nil
}

func (c *Config) normalizePaths() {
	c.BackupDir = filepath.FromSlash(filepath.Clean(c.BackupDir))
	c.LogFilename = filepath.FromSlash(filepath.Clean(c.LogFilename))
	c.RemoteBackupDir = filepath.FromSlash(filepath.Clean(c.RemoteBackupDir))
	if c.MySQLBin != "" {
		c.MySQLBin = filepath.FromSlash(filepath.Clean(c.MySQLBin))
	}
	if c.RemoteSSHKeyFile != "" {
		c.RemoteSSHKeyFile = filepath.FromSlash(filepath.Clean(c.RemoteSSHKeyFile))
	}
}

// LoadClean reads config and writes it back with plaintext passwords (for migration/inspection).
func LoadClean(path string) error {
	cfg := DefaultConfig()
	if err := sconfig.LoadConfig(cfg, cfg.Version, path, true, false); err != nil {
		return fmt.Errorf("sconfig load clean: %w", err)
	}
	return nil
}

// HostnameForBackup returns the hostname used for Backup-Dateinamen. Bei localhost/127.0.0.1 und gesetztem mysql_hostname wird dieser verwendet.
func (c *Config) HostnameForBackup() string {
	h := strings.TrimSpace(c.MySQLHost)
	if (h == "localhost" || h == "127.0.0.1") && strings.TrimSpace(c.MySQLHostname) != "" {
		return strings.TrimSpace(c.MySQLHostname)
	}
	if h == "" {
		return "localhost"
	}
	return h
}

// ConfigPath finds config file: -config flag, then current dir, then user home.
func ConfigPath(flagPath string) string {
	if flagPath != "" {
		return filepath.FromSlash(filepath.Clean(flagPath))
	}
	// Current directory
	const name = "mysqlbackup_config.json"
	if _, err := os.Stat(name); err == nil {
		return name
	}
	dir, err := os.Getwd()
	if err == nil {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// User home
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, name)
	}
	return name
}
