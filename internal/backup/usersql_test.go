package backup

import (
	"strings"
	"testing"
)

func TestParseUserSQL(t *testing.T) {
	sql := []byte(
		"CREATE USER 'u1'@'%' IDENTIFIED BY PASSWORD 'x';\n" +
			"GRANT ALL ON `db1`.* TO 'u1'@'%';\n" +
			"CREATE USER 'u2'@'localhost';\n" +
			"GRANT SELECT ON `db2`.* TO 'u2'@'localhost';\n" +
			"GRANT ALL ON `db1`.* TO 'u2'@'localhost';\n")
	out, userNames := ParseUserSQL(sql, nil)
	if out["db1"] == "" {
		t.Error("expected db1 to have user SQL")
	}
	if out["db2"] == "" {
		t.Error("expected db2 to have user SQL")
	}
	// userNames kommt aus derselben Struktur wie out (kein zweites Parsing)
	if len(userNames) < 2 {
		t.Errorf("expected at least 2 user names from same parse, got %v", userNames)
	}
	// db1 must contain only GRANTs for db1 (u1 and u2), not db2 GRANTs
	if len(out["db1"]) > 0 && strings.Contains(out["db1"], "ON `db2`.") {
		t.Error("db1 block must not contain GRANT for db2")
	}
	if len(out["db2"]) > 0 && strings.Contains(out["db2"], "ON `db1`.") {
		t.Error("db2 block must not contain GRANT for db1")
	}
	// CREATE USER must be rewritten to IF NOT EXISTS
	if len(out["db1"]) > 0 && !strings.Contains(out["db1"], "CREATE USER IF NOT EXISTS") {
		t.Error("expected CREATE USER IF NOT EXISTS in output")
	}
}

func TestParseUserSQL_empty(t *testing.T) {
	out, names := ParseUserSQL(nil, nil)
	if len(out) != 0 {
		t.Errorf("ParseUserSQL(nil): got %d entries", len(out))
	}
	if names != nil {
		t.Errorf("ParseUserSQL(nil): expected nil names, got %v", names)
	}
	out, names = ParseUserSQL([]byte{}, nil)
	if len(out) != 0 {
		t.Errorf("ParseUserSQL([]): got %d entries", len(out))
	}
	if names != nil {
		t.Errorf("ParseUserSQL([]): expected nil names, got %v", names)
	}
}

// TestParseUserSQL_quoteForms verifies that `name`, "name", 'name' and unquoted name all map to name.
func TestParseUserSQL_quoteForms(t *testing.T) {
	tests := []struct {
		sql    string
		expect string
	}{
		{"CREATE USER 'u1'@'%';\nGRANT ALL ON `db1`.* TO 'u1'@'%';\n", "u1"},
		{"CREATE USER `u2`@`localhost`;\nGRANT SELECT ON `db1`.* TO `u2`@`localhost`;\n", "u2"},
		{"CREATE USER \"u3\"@\"%\";\nGRANT ALL ON `db1`.* TO \"u3\"@\"%\";\n", "u3"},
		{"CREATE USER u4@localhost;\nGRANT SELECT ON `db1`.* TO u4@localhost;\n", "u4"},
	}
	for _, tt := range tests {
		out, _ := ParseUserSQL([]byte(tt.sql), nil)
		if out["db1"] == "" {
			t.Errorf("quote form %q: expected db1 to have SQL", tt.sql[:min(40, len(tt.sql))])
		}
		if out["db1"] != "" && !strings.Contains(out["db1"], tt.expect) {
			t.Errorf("quote form: expected output to contain %q", tt.expect)
		}
	}
}

// TestParseUserSQL_identifiedAndOnQuotes verifies IDENTIFIED BY PASSWORD and ON db.* with different quote styles.
func TestParseUserSQL_identifiedAndOnQuotes(t *testing.T) {
	// IDENTIFIED BY PASSWORD with single quote (existing), and ON with backticks
	sql := "CREATE USER 'u1'@'%' IDENTIFIED BY PASSWORD 'hash1';\nGRANT ALL ON `mydb`.* TO 'u1'@'%';\n"
	out, _ := ParseUserSQL([]byte(sql), nil)
	if out["mydb"] == "" {
		t.Error("expected mydb to have SQL")
	}
	if out["mydb"] != "" && !strings.Contains(out["mydb"], "hash1") {
		t.Error("expected IDENTIFIED BY PASSWORD hash in output")
	}
	// ON with double-quoted db name
	sql2 := "CREATE USER \"u2\"@\"%\";\nGRANT SELECT ON \"otherdb\".* TO \"u2\"@\"%\";\n"
	out2, _ := ParseUserSQL([]byte(sql2), nil)
	if out2["otherdb"] == "" {
		t.Error("expected otherdb (double-quoted ON) to have SQL")
	}
}
