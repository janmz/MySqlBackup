// Package mysql runs mysql/mysqldump/mysqlpump for listing DBs and exporting data/users.
package mysql

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Conn holds MySQL connection parameters for CLI invocations.
type Conn struct {
	Host     string
	Port     int
	User     string
	Password string
	BinDir   string // optional: Verzeichnis mit mysql, mysqldump, mysqlpump (leer = aus PATH)
}

// DSN returns host:port for use in -h.
func (c *Conn) hostPort() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// binPath returns the path to the given executable (mysql, mysqldump, mysqlpump). Wenn BinDir leer, nur Name (aus PATH); sonst voller Pfad.
func (c *Conn) binPath(name string) string {
	if strings.TrimSpace(c.BinDir) == "" {
		return name
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(c.BinDir, name+".exe")
	}
	return filepath.Join(c.BinDir, name)
}

// baseArgs returns common args for mysql/mysqldump (host, port, user, password).
func (c *Conn) baseArgs() []string {
	args := []string{
		"-h", c.Host,
		"-P", fmt.Sprintf("%d", c.Port),
		"-u", c.User,
	}
	if c.Password != "" {
		args = append(args, "-p"+c.Password)
	}
	return args
}

// Reachable returns nil if the server accepts connections (e.g. for lifecycle check before start).
func (c *Conn) Reachable() error {
	args := append(c.baseArgs(), "-e", "SELECT 1")
	cmd := exec.Command(c.binPath("mysql"), args...)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mysql reachable: %w (output: %s)", err, string(out))
	}
	return nil
}

// IsMariaDB returns true if the server is MariaDB (used to choose --system=users vs mysqlpump).
func (c *Conn) IsMariaDB() (bool, error) {
	args := append(c.baseArgs(), "-e", "SELECT @@version")
	cmd := exec.Command(c.binPath("mysql"), args...)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("mysql version: %w (output: %s)", err, string(out))
	}
	return strings.Contains(strings.ToLower(string(out)), "mariadb"), nil
}

// ListDatabases returns database names excluding information_schema, performance_schema, mysql.
func (c *Conn) ListDatabases() ([]string, error) {
	args := append(c.baseArgs(), "-e", "SHOW DATABASES")
	cmd := exec.Command(c.binPath("mysql"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("show databases: %w (output: %s)", err, string(out))
	}
	var dbs []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "Database" || line == "information_schema" || line == "performance_schema" || line == "mysql" {
			continue
		}
		dbs = append(dbs, line)
	}
	return dbs, sc.Err()
}

// ExportUsers runs mysqldump --system=users (MariaDB) or mysqlpump --users (MySQL), returns SQL.
func (c *Conn) ExportUsers(isMariaDB bool) ([]byte, error) {
	if isMariaDB {
		args := append(c.baseArgs(), "--system=users")
		cmd := exec.Command(c.binPath("mysqldump"), args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("mysqldump --system=users: %w (output: %s)", err, string(out))
		}
		return out, nil
	}
	// MySQL: mysqlpump --exclude-databases=% --users
	args := append(c.baseArgs(), "--exclude-databases=%", "--users")
	cmd := exec.Command(c.binPath("mysqlpump"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mysqlpump --users: %w (output: %s)", err, string(out))
	}
	return out, nil
}

// DumpDatabase runs mysqldump for one database; returns stdout (caller appends user block and zips).
// isMariaDB: bei true wird --set-gtid-purged=OFF weggelassen (nur MySQL, nicht MariaDB).
func (c *Conn) DumpDatabase(db string, isMariaDB bool) ([]byte, error) {
	args := append(c.baseArgs(),
		"--single-transaction",
		"--routines", "--triggers", "--events",
	)
	if !isMariaDB {
		args = append(args, "--set-gtid-purged=OFF")
	}
	args = append(args, "--databases", db)
	cmd := exec.Command(c.binPath("mysqldump"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mysqldump %s: %w (output: %s)", db, err, string(out))
	}
	return out, nil
}

// MysqldumpPath returns the name of mysqldump (or full path on Windows if in PATH).
func MysqldumpPath() string {
	if runtime.GOOS == "windows" {
		return "mysqldump.exe"
	}
	return "mysqldump"
}
