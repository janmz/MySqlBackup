// Package backup implements MySQL backup: dump, user append, zip.
package backup

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/mysql"
)

// hostnameForFile returns a safe filename part for backup names (no slashes, colons, etc.).
// Caller should pass cfg.HostnameForBackup() so localhost uses mysql_hostname when set.
func hostnameForFile(host string) string {
	if host == "" {
		host = "localhost"
	}
	host = regexp.MustCompile(`[^\w\-.]`).ReplaceAllString(host, "_")
	return host
}

// Run performs full backup: export users, parse, for each DB dump+append users+zip.
// isMariaDB: bei true wird --set-gtid-purged=OFF nicht an mysqldump übergeben (MariaDB kennt die Option nicht).
func Run(cfg *config.Config, conn *mysql.Conn, userSQL []byte, dbs []string, isMariaDB bool, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Error(string, ...interface{})
}) (createdFiles []string, err error) {
	backupDir := filepath.FromSlash(cfg.BackupDir)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	dateStr := time.Now().Format("20060102")
	hostPart := hostnameForFile(cfg.HostnameForBackup())
	dbToUserSQL := ParseUserSQL(userSQL)

	for _, db := range dbs {
		dump, err := conn.DumpDatabase(db, isMariaDB)
		if err != nil {
			return nil, fmt.Errorf("dump %s: %w", db, err)
		}
		log.Info("dumped database %s", db)

		var fullSQL strings.Builder
		fullSQL.Write(dump)
		if userBlock, ok := dbToUserSQL[db]; ok && userBlock != "" {
			fullSQL.WriteString("\n\n")
			fullSQL.WriteString(userBlock)
			fullSQL.WriteString("\n\nFLUSH PRIVILEGES;\n")
		}

		zipName := fmt.Sprintf("mysql_backup_%s_%s_%s.zip", dateStr, hostPart, db)
		zipPath := filepath.Join(backupDir, zipName)
		if err := writeZIP(zipPath, db+".sql", fullSQL.String()); err != nil {
			return nil, fmt.Errorf("zip %s: %w", db, err)
		}
		createdFiles = append(createdFiles, zipPath)
		log.Info("created %s", zipName)
	}
	return createdFiles, nil
}

func writeZIP(zipPath, entryName, content string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	wr, err := w.Create(entryName)
	if err != nil {
		return err
	}
	if _, err := wr.Write([]byte(content)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}
