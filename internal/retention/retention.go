// Package retention classifies backups by period and deletes excess.
package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const backupPrefix = "mysql_backup_"

var dateInFilename = regexp.MustCompile(`mysql_backup_(\d{8})_`)

// Period is daily, weekly, monthly, or yearly.
type Period int

const (
	Daily Period = iota
	Weekly
	Monthly
	Yearly
)

// Classify returns the single retention period for a date (yearly > monthly > weekly > daily).
// Yearly = 31.12, Monthly = last day of month (not 31.12), Weekly = Sunday (not month-end), Daily = rest.
func Classify(t time.Time) Period {
	if t.Month() == 12 && t.Day() == 31 {
		return Yearly
	}
	if isLastDayOfMonth(t) {
		return Monthly
	}
	if t.Weekday() == time.Sunday {
		return Weekly
	}
	return Daily
}

func isLastDayOfMonth(t time.Time) bool {
	next := t.AddDate(0, 0, 1) // next calendar day
	return next.Month() != t.Month()
}

// BackupFile holds path and parsed date for a backup zip.
type BackupFile struct {
	Path string
	Date time.Time
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
		t, err := time.Parse("20060102", matches[1])
		if err != nil {
			continue
		}
		files = append(files, BackupFile{Path: filepath.Join(dir, name), Date: t})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date.Before(files[j].Date) })
	return files, nil
}

// Apply deletes excess backups so that retain_daily/weekly/monthly/yearly are kept.
// Order: daily (keep last N), then weekly (keep last N), monthly, yearly.
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

	byPeriod := map[Period][]BackupFile{
		Daily:   nil,
		Weekly:  nil,
		Monthly: nil,
		Yearly:  nil,
	}
	for _, f := range files {
		p := Classify(f.Date)
		byPeriod[p] = append(byPeriod[p], f)
	}

	// For each period, keep only the last retain_*; delete the rest.
	for _, p := range []Period{Yearly, Monthly, Weekly, Daily} {
		list := byPeriod[p]
		if len(list) == 0 {
			continue
		}
		var toDelete int
		switch p {
		case Daily:
			toDelete = len(list) - retainDaily
		case Weekly:
			toDelete = len(list) - retainWeekly
		case Monthly:
			toDelete = len(list) - retainMonthly
		case Yearly:
			toDelete = len(list) - retainYearly
		}
		if toDelete <= 0 {
			continue
		}
		// Delete oldest toDelete files in this period
		for i := 0; i < toDelete && i < len(list); i++ {
			if err := os.Remove(list[i].Path); err != nil {
				log.Warn("retention delete %s: %v", list[i].Path, err)
				continue
			}
			log.Info("deleted old backup %s", filepath.Base(list[i].Path))
		}
	}
	return nil
}

// ApplyToDirs runs Apply on backupDir and optionally remoteBackupDir (if non-empty).
func ApplyToDirs(backupDir, remoteBackupDir string, retainDaily, retainWeekly, retainMonthly, retainYearly int, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) error {
	if err := Apply(backupDir, retainDaily, retainWeekly, retainMonthly, retainYearly, log); err != nil {
		return fmt.Errorf("retention local: %w", err)
	}
	if remoteBackupDir != "" {
		if err := Apply(remoteBackupDir, retainDaily, retainWeekly, retainMonthly, retainYearly, log); err != nil {
			return fmt.Errorf("retention remote: %w", err)
		}
	}
	return nil
}
