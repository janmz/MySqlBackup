// Package config loads and holds MySQL backup configuration via janmz/sconfig (JSON).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/janmz/sconfig"
)

// Config holds all settings for MySQL backup (JSON with sconfig secure password pairs).
type Config struct {
	Version int `json:"version"`

	MySQLHost      string `json:"mysql_host"`
	MySQLHostname  string `json:"mysql_hostname"` // optional: für Benennung (Backup-Dateien), wenn mysql_host = localhost
	MySQLPort      int    `json:"mysql_port"`
	MySQLBin       string `json:"mysql_bin"`        // optional: Verzeichnis mit mysql, mysqldump, mysqlpump (z. B. D:\xampp\mysql\bin)
	MySQLDataDir   string `json:"mysql_data_dir"`   // Pfad zum data-Verzeichnis der Instanz (für -restorefull)
	MySQLBackupDir string `json:"mysql_backup_dir"` // optional: Pfad zum backup-Verzeichnis der Instanz (für -restorefull), leer = Nachbar von mysql_data_dir

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

	AdminEmail              string `json:"admin_email"`
	AdminSMTPServer         string `json:"admin_smtp_server"`
	AdminSMTPPort           int    `json:"admin_smtp_port"`
	AdminSMTPUser           string `json:"admin_smtp_user"` // optional: Login (wenn leer = admin_email)
	AdminSMTPTLS            string `json:"admin_smtp_tls"`  // "tls" (implizit, Port 465), "starttls" (Port 587), "" = Auto
	AdminSMTPPassword       string `json:"admin_smtp_password"`
	AdminSMTPSecurePassword string `json:"admin_smtp_secure_password"`

	RemoteBackupDir         string `json:"remote_backup_dir"`
	RemoteSSHHost           string `json:"remote_ssh_host"`
	RemoteSSHPort           int    `json:"remote_ssh_port"`
	RemoteSSHUser           string `json:"remote_ssh_user"`
	RemoteSSHPassword       string `json:"remote_ssh_password"`
	RemoteSSHSecurePassword string `json:"remote_ssh_secure_password"`
	RemoteSSHKeyFile   string `json:"remote_ssh_key_file"`
	RemoteSSHHostKey   string `json:"remote_ssh_host_key"` // path to known_hosts (or file with host key line) OR inline key "key-type base64..."

	// Optional: Remote-Dateien vor Upload mit AES-256 verschlüsseln. Schlüssel aus remote_aes_password abgeleitet.
	// Wenn entschlüsselter Wert "" ist, erfolgt keine Verschlüsselung.
	RemoteAESPassword       string `json:"remote_aes_password"`
	RemoteAESSecurePassword string `json:"remote_aes_secure_password"`

	StartTime string `json:"start_time"`
}

// DefaultConfig returns config with default values.
func DefaultConfig() *Config {
	return &Config{
		MySQLPort:     3306,
		RetainDaily:   14,
		RetainWeekly:  3,
		RetainMonthly: 3,
		RetainYearly:  3,
		AdminSMTPPort: 587,
		RemoteSSHPort: 22,
		StartTime:     "22:00",
	}
}

const maxConfigSize = 10 * 1024 // 10 KiB

// Load reads config from path via sconfig (JSON + secure passwords), then normalizes paths.
// Config file must not exceed 10 KiB. If cleanConfig is true, sconfig writes the file back with plaintext passwords (for migration/inspection).
func Load(path string, cleanConfig bool) (*Config, error) {
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxConfigSize {
		return nil, fmt.Errorf(i18n.T("err.config_too_large"), fi.Size(), maxConfigSize)
	}

	var debugSconfig bool = false

	if debugSconfig {
		id, err := sconfig.DebugHardwareID()
		if err != nil {
			return nil, fmt.Errorf(i18n.T("err.sconfig_hw"), err)
		}
		fmt.Println(i18n.Tf("log.debug.hardware_id", id))
	}

	cfg := DefaultConfig()
	if err := sconfig.LoadConfig(cfg, cfg.Version, path, cleanConfig, debugSconfig); err != nil {
		return nil, fmt.Errorf(i18n.T("err.sconfig_load"), err)
	}
	cfg.normalizePaths()
	return cfg, nil
}

// Validate returns warnings for config values outside recommended bounds (ports 1024–65535, retain 1–364, start_time 00:00–23:59).
func Validate(cfg *Config) []string {
	var w []string
	// Ports: > 1023 and < 65536
	if cfg.MySQLPort <= 1023 || cfg.MySQLPort >= 65536 {
		w = append(w, i18n.Tf("config.warn.port_range", "mysql_port", cfg.MySQLPort, 1024, 65535))
	}
	if cfg.AdminSMTPPort != 0 && (cfg.AdminSMTPPort <= 1023 || cfg.AdminSMTPPort >= 65536) {
		w = append(w, i18n.Tf("config.warn.port_range", "admin_smtp_port", cfg.AdminSMTPPort, 1024, 65535))
	}
	if cfg.RemoteSSHPort != 0 && (cfg.RemoteSSHPort <= 1023 || cfg.RemoteSSHPort >= 65536) {
		w = append(w, i18n.Tf("config.warn.port_range", "remote_ssh_port", cfg.RemoteSSHPort, 1024, 65535))
	}
	// Retain: > 0 and < 365
	if cfg.RetainDaily <= 0 || cfg.RetainDaily >= 365 {
		w = append(w, i18n.Tf("config.warn.retain_range", "retain_daily", cfg.RetainDaily, 1, 364))
	}
	if cfg.RetainWeekly <= 0 || cfg.RetainWeekly >= 365 {
		w = append(w, i18n.Tf("config.warn.retain_range", "retain_weekly", cfg.RetainWeekly, 1, 364))
	}
	if cfg.RetainMonthly <= 0 || cfg.RetainMonthly >= 365 {
		w = append(w, i18n.Tf("config.warn.retain_range", "retain_monthly", cfg.RetainMonthly, 1, 364))
	}
	if cfg.RetainYearly <= 0 || cfg.RetainYearly >= 365 {
		w = append(w, i18n.Tf("config.warn.retain_range", "retain_yearly", cfg.RetainYearly, 1, 364))
	}
	// StartTime: 00:00–23:59
	st := strings.TrimSpace(cfg.StartTime)
	if len(st) == 5 && st[2] == ':' {
		var hour, min int
		if _, err := fmt.Sscanf(st[:2], "%d", &hour); err == nil {
			if _, err := fmt.Sscanf(st[3:5], "%d", &min); err == nil {
				if hour < 0 || hour > 23 || min < 0 || min > 59 {
					w = append(w, i18n.T("config.warn.start_time"))
				}
			} else {
				w = append(w, i18n.T("config.warn.start_time"))
			}
		} else {
			w = append(w, i18n.T("config.warn.start_time"))
		}
	} else if st != "" {
		w = append(w, i18n.T("config.warn.start_time"))
	}
	return w
}

func (c *Config) normalizePaths() {
	c.BackupDir = filepath.FromSlash(filepath.Clean(c.BackupDir))
	c.LogFilename = filepath.FromSlash(filepath.Clean(c.LogFilename))
	c.RemoteBackupDir = filepath.FromSlash(filepath.Clean(c.RemoteBackupDir))
	if c.MySQLBin != "" {
		c.MySQLBin = filepath.FromSlash(filepath.Clean(c.MySQLBin))
	}
	if c.MySQLDataDir != "" {
		c.MySQLDataDir = filepath.FromSlash(filepath.Clean(c.MySQLDataDir))
	}
	if c.MySQLBackupDir != "" {
		c.MySQLBackupDir = filepath.FromSlash(filepath.Clean(c.MySQLBackupDir))
	}
	if c.RemoteSSHKeyFile != "" {
		c.RemoteSSHKeyFile = filepath.FromSlash(filepath.Clean(c.RemoteSSHKeyFile))
	}
}

// LoadClean reads config and writes it back with plaintext passwords (for migration/inspection).
// If debug is true, sconfig may print debug output (e.g. when -verbose is used).
func LoadClean(path string, debug bool) error {
	cfg := DefaultConfig()
	if err := sconfig.LoadConfig(cfg, cfg.Version, path, true, debug); err != nil {
		return fmt.Errorf(i18n.T("err.sconfig_clean"), err)
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

// ConfigPath finds config file: -config flag, then invoked dir (where symlink lives), then executable dir (resolved binary), then current dir, then user home.
// invokedDir should be the directory of the path used to start the program (e.g. dir of ./mysqlbackup); empty if started by name from PATH.
// This way, when running ./mysqlbackup from a subdir where mysqlbackup is a symlink to the parent, config is taken from the subdir, not from the link target.
func ConfigPath(flagPath string, invokedDir string) string {
	if flagPath != "" {
		return filepath.FromSlash(filepath.Clean(flagPath))
	}
	const name = "config.json"
	// Invoked directory first (where the symlink lives; avoids using Windows config when running ./mysqlbackup from Linux subdir)
	if invokedDir != "" {
		p := filepath.Join(invokedDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Default: config next to invoked path even if file does not exist yet
		return p
	}
	// Executable directory (resolved path of binary)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		p := filepath.Join(exeDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return p
	}
	// Fallback: current directory
	if _, err := os.Stat(name); err == nil {
		return name
	}
	if dir, err := os.Getwd(); err == nil {
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
