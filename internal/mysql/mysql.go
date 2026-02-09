// Package mysql runs mysql/mysqldump/mysqlpump for listing DBs and exporting data/users.
package mysql

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janmz/mysqlbackup/internal/i18n"
)

// Conn holds MySQL connection parameters for CLI invocations.
type Conn struct {
	Host     string
	Port     int
	User     string
	Password string
	BinDir   string // optional: Verzeichnis mit mysql, mysqldump, mysqlpump (leer = aus PATH)
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
		return fmt.Errorf(i18n.T("err.mysql_reachable"), err, string(out))
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
		return false, fmt.Errorf(i18n.T("err.mysql_version"), err, string(out))
	}
	return strings.Contains(strings.ToLower(string(out)), "mariadb"), nil
}

// ListDatabases returns database names excluding system schemas: information_schema, performance_schema, mysql, sys.
func (c *Conn) ListDatabases() ([]string, error) {
	args := append(c.baseArgs(), "-e", "SHOW DATABASES")
	cmd := exec.Command(c.binPath("mysql"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(i18n.T("err.show_databases"), err, string(out))
	}
	var dbs []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "Database" || line == "information_schema" || line == "performance_schema" || line == "mysql" || line == "sys" {
			continue
		}
		dbs = append(dbs, line)
	}
	return dbs, sc.Err()
}

// ExportUsers runs mysqldump --system=users (MariaDB, wo unterstützt) oder mysqlpump --users (MySQL), returns SQL.
// MariaDB: Wenn --system=users nicht unterstützt wird (z. B. vor 10.2.37), Fallback per mysql.user + SHOW GRANTS.
func (c *Conn) ExportUsers(isMariaDB bool) ([]byte, error) {
	if isMariaDB {
		out, err := c.exportUsersMariaDB()
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	// MySQL: mysqlpump --exclude-databases=% --users
	args := append(c.baseArgs(), "--exclude-databases=%", "--users")
	cmd := exec.Command(c.binPath("mysqlpump"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(i18n.T("err.mysqlpump_users"), err, string(out))
	}
	return out, nil
}

// exportUsersMariaDB tries mysqldump --system=users; if the option is not supported (z. B. ältere MariaDB),
// fallback to exporting users via mysql.user + SHOW GRANTS.
func (c *Conn) exportUsersMariaDB() ([]byte, error) {
	args := append(c.baseArgs(), "--system=users")
	cmd := exec.Command(c.binPath("mysqldump"), args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	errStr := strings.ToLower(string(out))
	if strings.Contains(errStr, "unknown") || strings.Contains(errStr, "unrecognized") ||
		strings.Contains(errStr, "unknown variable") || strings.Contains(errStr, "invalid") {
		return c.exportUsersMariaDBFallback()
	}
	return nil, fmt.Errorf(i18n.T("err.mysqldump_system_users"), err, string(out))
}

// exportUsersMariaDBFallback exports users via SELECT from mysql.user and SHOW GRANTS FOR each user.
// Output format matches what our backup parser expects (CREATE USER + GRANT lines).
func (c *Conn) exportUsersMariaDBFallback() ([]byte, error) {
	// List users (skip root and system users)
	q := "SELECT user, host, plugin, COALESCE(authentication_string,'') FROM mysql.user WHERE user != '' AND user NOT IN ('root','mysql.sys','mysql.session','mariadb.sys')"
	args := append(c.baseArgs(), "-N", "-e", q)
	cmd := exec.Command(c.binPath("mysql"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(i18n.T("err.mysql_user_list"), err, string(out))
	}
	var buf strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 512*1024)
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		user, host, plugin, auth := parts[0], parts[1], strings.TrimSpace(parts[2]), parts[3]
		userEsc := strings.ReplaceAll(user, "\\", "\\\\")
		userEsc = strings.ReplaceAll(userEsc, "'", "''")
		hostEsc := strings.ReplaceAll(host, "\\", "\\\\")
		hostEsc = strings.ReplaceAll(hostEsc, "'", "''")
		// CREATE USER (compatible with our parser)
		if plugin != "" && plugin != "mysql_native_password" {
			authEsc := strings.ReplaceAll(auth, "\\", "\\\\")
			authEsc = strings.ReplaceAll(authEsc, "'", "''")
			fmt.Fprintf(&buf, "CREATE USER '%s'@'%s' IDENTIFIED WITH %s AS '%s';\n", userEsc, hostEsc, plugin, authEsc)
		} else if auth != "" {
			authEsc := strings.ReplaceAll(auth, "\\", "\\\\")
			authEsc = strings.ReplaceAll(authEsc, "'", "''")
			fmt.Fprintf(&buf, "CREATE USER '%s'@'%s' IDENTIFIED BY PASSWORD '%s';\n", userEsc, hostEsc, authEsc)
		} else {
			fmt.Fprintf(&buf, "CREATE USER '%s'@'%s';\n", userEsc, hostEsc)
		}
		// SHOW GRANTS FOR 'user'@'host'
		showQ := fmt.Sprintf("SHOW GRANTS FOR '%s'@'%s'", userEsc, hostEsc)
		args := append(c.baseArgs(), "-N", "-e", showQ)
		cmd := exec.Command(c.binPath("mysql"), args...)
		grantOut, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		gr := bufio.NewScanner(bytes.NewReader(grantOut))
		for gr.Scan() {
			g := strings.TrimSpace(gr.Text())
			if g != "" {
				buf.WriteString(g)
				if !strings.HasSuffix(g, ";") {
					buf.WriteString(";")
				}
				buf.WriteString("\n")
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf(i18n.T("err.scan_user_list"), err)
	}
	return []byte(buf.String()), nil
}

// DumpDatabase streams mysqldump output for one database into dest. Kein vollständiger Dump im Speicher.
// isMariaDB: bei true wird --set-gtid-purged=OFF weggelassen (nur MySQL, nicht MariaDB).
func (c *Conn) DumpDatabase(db string, isMariaDB bool, dest io.Writer) error {
	args := append(c.baseArgs(),
		"--single-transaction",
		"--routines", "--triggers", "--events",
	)
	if !isMariaDB {
		args = append(args, "--set-gtid-purged=OFF")
	}
	args = append(args, "--databases", db)
	cmd := exec.Command(c.binPath("mysqldump"), args...)
	cmd.Stdout = dest
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(i18n.Tf("err.mysqldump_db", db), err, stderr.String())
	}
	return nil
}

// MysqldumpPath returns the name of mysqldump (or full path on Windows if in PATH).
func MysqldumpPath() string {
	if runtime.GOOS == "windows" {
		return "mysqldump.exe"
	}
	return "mysqldump"
}
