// Package retention classifies backups by period and deletes excess.
package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/janmz/mysqlbackup/internal/i18n"
)

const backupPrefix = "mysql_backup_"

var dateInFilename = regexp.MustCompile(`mysql_backup_(\d{8})_`)

// Classify returns the retention period for a date as a localized string (e.g. German "täglichen", "wöchentlichen").
// Order: yearly (31.12) > monthly (last day of month, not 31.12) > weekly (Sunday) > daily (rest).
func Classify(t time.Time) string {
	if t.Month() == 12 && t.Day() == 31 {
		return i18n.T("retention.yearly")
	}
	if isLastDayOfMonth(t) {
		return i18n.T("retention.monthly")
	}
	if t.Weekday() == time.Sunday {
		return i18n.T("retention.weekly")
	}
	return i18n.T("retention.daily")
}

func isLastDayOfMonth(t time.Time) bool {
	next := t.AddDate(0, 0, 1) // next calendar day
	return next.Month() != t.Month()
}

// BackupFile holds path, parsed date, file modification time and size for a backup zip.
type BackupFile struct {
	Path    string
	Date    time.Time
	ModTime time.Time
	Size    int64
}

// ListBackups returns all mysql_backup_*.zip in dir with parsed dates, sorted by date ascending.
func ListBackups(dir string) ([]BackupFile, error) {
	dir = filepath.FromSlash(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []BackupFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < len(backupPrefix)+8+2 || !regexp.MustCompile(`^mysql_backup_\d{8}_`).MatchString(name) || filepath.Ext(name) != ".zip" {
			continue
		}
		matches := dateInFilename.FindStringSubmatch(name)
		if len(matches) < 2 {
			continue
		}
		t, err := time.ParseInLocation("20060102", matches[1], time.Local)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(dir, name)
		bf := BackupFile{Path: fullPath, Date: t}
		if info, err := os.Stat(fullPath); err == nil {
			bf.ModTime = info.ModTime()
			bf.Size = info.Size()
		}
		files = append(files, bf)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date.Before(files[j].Date) })
	return files, nil
}

// LastBackupBefore returns all backup ZIPs from one backup day.
// If beforeDate is nil, it returns all files from the latest day.
// If beforeDate is set, it returns all files from the latest day strictly before beforeDate.
func LastBackupBefore(dir string, beforeDate *time.Time) ([]BackupFile, error) {
	files, err := ListBackups(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var target time.Time
	found := false
	if beforeDate == nil {
		target = files[len(files)-1].Date
		found = true
	} else {
		for i := len(files) - 1; i >= 0; i-- {
			if files[i].Date.Before(*beforeDate) {
				target = files[i].Date
				found = true
				break
			}
		}
	}
	if !found {
		return nil, nil
	}

	key := dateKey(target)
	var selected []BackupFile
	for _, f := range files {
		if dateKey(f.Date) == key {
			selected = append(selected, f)
		}
	}
	return selected, nil
}

// dateKey returns YYYYMMDD for use as a map key (same timezone as t).
func dateKey(t time.Time) string {
	return t.Format("20060102")
}

// Apply deletes backups that fall outside the retention windows.
// retain_daily 14 = keep all daily backups from the last 14 calendar days (by backup date).
// retain_weekly 3 = keep all weekly backups from the last 3 Sundays; retain_monthly/yearly = last N month-ends / year-ends.
// So we delete by date window, not by "last N files", so multiple DBs per day/week are all kept within the window.
func Apply(dir string, retainDaily, retainWeekly, retainMonthly, retainYearly int, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) error {
	files, err := ListBackups(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Cutoff: keep daily backups with date >= today - retainDaily
	dailyCutoff := today.AddDate(0, 0, -retainDaily)

	// Last N Sundays (set of dates to keep for weekly)
	keepSundays := make(map[string]bool)
	// Finde den letzten Sonntag (inklusive heute, falls heute Sonntag ist)
	lastSunday := today
	for lastSunday.Weekday() != time.Sunday {
		lastSunday = lastSunday.AddDate(0, 0, -1)
	}
	// Gehe jeweils 7 Tage nach vorne, retainWeekly-mal
	for i := 0; i < retainWeekly; i++ {
		sunday := lastSunday.AddDate(0, 0, 7*i)
		keepSundays[dateKey(sunday)] = true
		if sunday.Year() < 2000 {
			break
		}
	}

	// Last N month-ends (not 31.12; those are yearly)
	keepMonthEnds := make(map[string]bool)
	// Gehe nur die Monate zurück und berechne das Monatsende jedes Monats.
	for m, count := today, 0; count < retainMonthly; m = m.AddDate(0, -1, 0) {
		// Monatsende berechnen: zum 1. des nächsten Monats springen, dann einen Tag zurück.
		firstOfNextMonth := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, m.Location()).AddDate(0, 1, 0)
		monthEnd := firstOfNextMonth.AddDate(0, 0, -1)
		keepMonthEnds[dateKey(monthEnd)] = true
		count++
		if monthEnd.Year() < 2000 {
			break
		}
	}

	// Last N year-ends (31.12)
	keepYearEnds := make(map[string]bool)
	for y, count := today.Year(), 0; count < retainYearly && y >= 2000; y, count = y-1, count+1 {
		lastDay := time.Date(y, 12, 31, 0, 0, 0, 0, today.Location())
		keepYearEnds[dateKey(lastDay)] = true
	}

	for _, f := range files {
		key := dateKey(f.Date)
		keep := !f.Date.Before(dailyCutoff)
		keep = keep || keepSundays[key]
		keep = keep || keepMonthEnds[key]
		keep = keep || keepYearEnds[key]
		if keep {
			continue
		}
		if err := os.Remove(f.Path); err != nil {
			log.Warn(i18n.Tf("log.warn.retention_delete", f.Path, err))
			continue
		}
		log.Info(i18n.Tf("log.msg.deleted_old_backup", Classify(f.Date), filepath.Base(f.Path)))
	}
	return nil
}

// ApplyToDirs runs Apply on backupDir and optionally remoteBackupDir (if non-empty).
func ApplyToDirs(backupDir, remoteBackupDir string, retainDaily, retainWeekly, retainMonthly, retainYearly int, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) error {
	if err := Apply(backupDir, retainDaily, retainWeekly, retainMonthly, retainYearly, log); err != nil {
		return fmt.Errorf(i18n.T("err.retention_local"), err)
	}
	if remoteBackupDir != "" {
		if err := Apply(remoteBackupDir, retainDaily, retainWeekly, retainMonthly, retainYearly, log); err != nil {
			return fmt.Errorf(i18n.T("err.retention_remote"), err)
		}
	}
	return nil
}
