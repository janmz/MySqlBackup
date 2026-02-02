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
	out := ParseUserSQL(sql)
	if out["db1"] == "" {
		t.Error("expected db1 to have user SQL")
	}
	if out["db2"] == "" {
		t.Error("expected db2 to have user SQL")
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
	out := ParseUserSQL(nil)
	if len(out) != 0 {
		t.Errorf("ParseUserSQL(nil): got %d entries", len(out))
	}
	out = ParseUserSQL([]byte{})
	if len(out) != 0 {
		t.Errorf("ParseUserSQL([]): got %d entries", len(out))
	}
}
