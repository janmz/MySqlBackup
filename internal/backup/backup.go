// Package backup implements MySQL backup: dump, user append, zip.
package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
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
		return nil, fmt.Errorf(i18n.T("err.create_backup_dir"), err)
	}

	recoverSavFiles(backupDir, log)

	dateStr := time.Now().Format("20060102")
	hostPart := hostnameForFile(cfg.HostnameForBackup())
	dbToUserSQL, userNames := ParseUserSQL(userSQL, log.Warn)
	if len(userNames) > 0 {
		log.Info(i18n.Tf("log.msg.users_found", len(userNames), strings.Join(userNames, ", ")))
	}

	for _, db := range dbs {
		zipName := fmt.Sprintf("mysql_backup_%s_%s_%s.zip", dateStr, hostPart, db)
		zipPath := filepath.Join(backupDir, zipName)
		entryWriter, finish, cancel, err := safeWriteZIPStreaming(zipPath, db+".sql", log)
		if err != nil {
			return nil, fmt.Errorf(i18n.Tf("err.zip_db", db), err)
		}
		if err := conn.DumpDatabase(db, isMariaDB, entryWriter); err != nil {
			cancel()
			return nil, fmt.Errorf(i18n.Tf("err.dump_db", db), err)
		}
		log.Info(i18n.Tf("log.msg.dumped_db", db))
		userBlock, _ := dbToUserSQL[db]
		if userBlock != "" {
			if _, err := io.WriteString(entryWriter, "\n\n"); err != nil {
				cancel()
				return nil, fmt.Errorf(i18n.Tf("err.zip_user_block", db), err)
			}
			if _, err := io.WriteString(entryWriter, userBlock); err != nil {
				cancel()
				return nil, fmt.Errorf(i18n.Tf("err.zip_user_block", db), err)
			}
			if _, err := io.WriteString(entryWriter, "\n\nFLUSH PRIVILEGES;\n"); err != nil {
				cancel()
				return nil, fmt.Errorf(i18n.Tf("err.zip_user_block", db), err)
			}
		}
		// Nur im Erfolgsfall: ZIP schließen und .sav löschen
		if err := finish(); err != nil {
			cancel()
			return nil, fmt.Errorf(i18n.Tf("err.zip_db", db), err)
		}
		createdFiles = append(createdFiles, zipPath)
		log.Info(i18n.Tf("log.msg.created_zip", zipName))
	}
	return createdFiles, nil
}

// recoverSavFiles runs at backup start: for each leftover *.sav in backupDir, if the
// corresponding .zip exists keep the larger file; if only .sav exists, rename it to .zip.
func recoverSavFiles(backupDir string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		log.Warn(i18n.Tf("log.warn.recover_sav_read", err))
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sav") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sav")
		savPath := filepath.Join(backupDir, e.Name())
		zipPath := filepath.Join(backupDir, base+".zip")
		savInfo, errSav := os.Stat(savPath)
		if errSav != nil {
			continue
		}
		zipInfo, errZip := os.Stat(zipPath)
		if errZip != nil {
			if os.IsNotExist(errZip) {
				if err := os.Rename(savPath, zipPath); err != nil {
					log.Warn(i18n.Tf("log.warn.recover_sav_rename", e.Name(), base+".zip", err))
				} else {
					log.Info(i18n.Tf("log.msg.recovered", base+".zip"))
				}
			}
			continue
		}
		if savInfo.Size() >= zipInfo.Size() {
			if err := os.Remove(zipPath); err != nil {
				log.Warn(i18n.Tf("log.warn.recover_sav_remove", base+".zip", err))
				continue
			}
			if err := os.Rename(savPath, zipPath); err != nil {
				log.Warn(i18n.Tf("log.warn.recover_sav_rename2", e.Name(), base+".zip", err))
			} else {
				log.Info(i18n.Tf("log.msg.recovered_larger", base+".zip"))
			}
		} else {
			if err := os.Remove(savPath); err != nil {
				log.Warn(i18n.Tf("log.warn.recover_sav_remove", e.Name(), err))
			} else {
				log.Info(i18n.Tf("log.msg.removed_sav", e.Name()))
			}
		}
	}
}

// safeWriteZIPStreaming prepares a zip for streaming: renames existing to .sav, creates zip and entry.
// Returns entry writer, finish (close zip and file, remove .sav), cancel (remove zip, restore .sav).
// Caller streams dump to entryWriter, appends user block, then calls finish() or cancel() on error.
func safeWriteZIPStreaming(zipPath, entryName string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) (entryWriter io.Writer, finish func() error, cancel func(), err error) {
	savPath := strings.TrimSuffix(zipPath, ".zip") + ".sav"
	if _, statErr := os.Stat(zipPath); statErr == nil {
		if renameErr := os.Rename(zipPath, savPath); renameErr != nil {
			return nil, nil, nil, fmt.Errorf(i18n.T("err.rename_sav"), renameErr)
		}
	}
	f, err := os.Create(zipPath)
	if err != nil {
		if _, e := os.Stat(savPath); e == nil {
			_ = os.Rename(savPath, zipPath)
		}
		return nil, nil, nil, err
	}
	w := zip.NewWriter(f)
	wr, err := w.Create(entryName)
	if err != nil {
		_ = w.Close()
		_ = f.Close()
		_ = os.Remove(zipPath)
		if _, e := os.Stat(savPath); e == nil {
			_ = os.Rename(savPath, zipPath)
		}
		return nil, nil, nil, err
	}
	finish = func() error {
		if err := w.Close(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		// Neue ZIP erfolgreich geschrieben → evtl. angelegte .sav-Datei löschen
		_ = os.Remove(savPath)
		return nil
	}
	cancel = func() {
		_ = w.Close()
		_ = f.Close()
		_ = os.Remove(zipPath)
		if _, e := os.Stat(savPath); e == nil {
			if renameErr := os.Rename(savPath, zipPath); renameErr != nil {
				log.Warn(i18n.Tf("log.warn.restore_sav", renameErr))
			} else {
				log.Warn(i18n.Tf("log.warn.restored_sav", filepath.Base(zipPath)))
			}
		}
	}
	return wr, finish, cancel, nil
}
