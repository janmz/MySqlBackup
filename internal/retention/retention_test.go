package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janmz/mysqlbackup/internal/i18n"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		t    time.Time
		want string // i18n key (retention.daily etc.)
	}{
		{time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), "retention.yearly"},
		{time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC), "retention.monthly"},
		{time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC), "retention.monthly"},
		{time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC), "retention.weekly"},  // Sunday
		{time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), "retention.daily"}, // Wednesday
	}
	for _, tt := range tests {
		got := Classify(tt.t)
		want := i18n.T(tt.want)
		if got != want {
			t.Errorf("Classify(%v) = %q, want %q (key %s)", tt.t, got, want, tt.want)
		}
	}
}

func TestListBackups(t *testing.T) {
	dir := t.TempDir()
	// Create fake backup files
	for _, name := range []string{
		"mysql_backup_20250101_localhost_db1.zip",
		"mysql_backup_20250102_localhost_db1.zip",
		"other.zip",
		"mysql_backup_20250103_other_db2.zip",
	} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)
	}
	files, err := ListBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("ListBackups: got %d files, want 3", len(files))
	}
	if len(files) >= 1 && files[0].Date.Year() != 2025 {
		t.Errorf("first file date: %v", files[0].Date)
	}
}

func TestApplyRetentionByDateWindow(t *testing.T) {
	dir := t.TempDir()
	log := &testLogger{t: t}
	// Backup from 3 days ago (would be classified Weekly if Sunday, Daily otherwise)
	threeDaysAgo := time.Now().AddDate(0, 0, -3)
	dateStr := threeDaysAgo.Format("20060102")
	name := "mysql_backup_" + dateStr + "_host_db.zip"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// retain_daily 14 = keep last 14 days; 3-day-old backup must be kept
	err := Apply(dir, 14, 3, 3, 3, log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("retention deleted 3-day-old backup (retain_daily=14); should keep backups from last 14 days")
	}
}

type testLogger struct{ t *testing.T }

func (l *testLogger) Info(format string, args ...interface{}) { l.t.Logf("[INFO] "+format, args...) }
func (l *testLogger) Warn(format string, args ...interface{}) { l.t.Logf("[WARN] "+format, args...) }
