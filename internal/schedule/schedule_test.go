package schedule

import "testing"

func TestTaskRunUsesCmd(t *testing.T) {
	tests := []struct {
		run  string
		want bool
	}{
		{`cmd.exe /c cd /d "\\srv\share\dir" && "\\srv\share\dir\mysqlbackup.exe" --backup -config "\\srv\share\dir\config.json"`, true},
		{`"cmd.exe" /c cd /d "C:\dir" && "C:\dir\mysqlbackup.exe" --backup -config "C:\dir\config.json"`, true},
		{`CMD /c "cd /d \"C:\dir\" && \"C:\dir\mysqlbackup.exe\""`, true},
		{`"\\srv\share\Jans Dateien\mysqlbackup.exe" --backup -config "\\srv\share\Jans Dateien\config.json"`, false},
		{`"C:\cmdtools\mysqlbackup.exe" --backup -config "C:\cmdtools\config.json"`, false},
	}
	for _, tt := range tests {
		if got := taskRunUsesCmd(tt.run); got != tt.want {
			t.Errorf("taskRunUsesCmd(%q) = %v, want %v", tt.run, got, tt.want)
		}
	}
}

func TestParseTaskRunToExeConfigDirect(t *testing.T) {
	run := `"\\elisa\daten\Jans Dateien\MySqlBackup\mysqlbackup.exe" --backup -config "\\elisa\daten\Jans Dateien\MySqlBackup\config.json"`
	exe, cfg, ok := parseTaskRunToExeConfig(run)
	if !ok {
		t.Fatalf("parseTaskRunToExeConfig(%q) not ok", run)
	}
	if exe != `\\elisa\daten\Jans Dateien\MySqlBackup\mysqlbackup.exe` {
		t.Errorf("exe = %q", exe)
	}
	if cfg != `\\elisa\daten\Jans Dateien\MySqlBackup\config.json` {
		t.Errorf("config = %q", cfg)
	}
}

func TestWindowsTaskInfoMatchesRejectsCmdWrapper(t *testing.T) {
	exe := `\\srv\share\dir\mysqlbackup.exe`
	cfg := `\\srv\share\dir\config.json`
	info := windowsTaskInfo{
		Hostname:     "HOST",
		Status:       "Bereit",
		ScheduleType: "Täglich",
		StartTime:    "22:00:00",
		Days:         "Jeden 1 Tag(e)",
	}
	info.TaskToRun = `cmd.exe /c cd /d "\\srv\share\dir" && "` + exe + `" --backup -config "` + cfg + `"`
	if windowsTaskInfoMatches(info, "HOST", exe, cfg, "22:00") {
		t.Error("cmd.exe-wrapped task must not match (needs recreation)")
	}
	info.TaskToRun = `"` + exe + `" --backup -config "` + cfg + `"`
	if !windowsTaskInfoMatches(info, "HOST", exe, cfg, "22:00") {
		t.Error("direct task should match")
	}
}
