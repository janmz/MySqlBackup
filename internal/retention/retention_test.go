package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		t      time.Time
		expect Period
	}{
		{time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), Yearly},
		{time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC), Monthly},
		{time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC), Monthly},
		{time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC), Weekly}, // Sunday
		{time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), Daily},  // Wednesday
	}
	for _, tt := range tests {
		got := Classify(tt.t)
		if got != tt.expect {
			t.Errorf("Classify(%v) = %v, want %v", tt.t, got, tt.expect)
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
