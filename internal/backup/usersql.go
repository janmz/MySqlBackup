// Package backup implements MySQL backup: user export parsing, dump, append, zip.
//
// Gültige MySQL-/MariaDB-Identifikatoren (unquoted, MySQL 8.4 / MariaDB 10 Doku):
//   - ASCII: [0-9a-zA-Z], $, _
//   - Unicode BMP: U+0080..U+FFFF (Extended)
//   - Darf mit Ziffer beginnen, darf nicht nur aus Ziffern bestehen.
//
// Einfache Regex (nur ASCII): (?![0-9]+$)[a-zA-Z0-9$_]+
// Mit Unicode BMP (Go regexp \x{XXXX}): [a-zA-Z0-9$_\x{80}-\x{FFFF}]+
package backup

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/janmz/mysqlbackup/internal/i18n"
)

// UserBlock is one user's CREATE USER and GRANT statements (as raw SQL lines).
// Kept for compatibility; parsing is now done via parseUserRecords.
type UserBlock struct {
	SQL string
	DBs []string
}

var (
	// user@host: erlaubt sind `name`, "name", 'name' oder name (unquoted); Anführungszeichen müssen matchen.
	// Unquoted = ASCII [0-9a-zA-Z$_] + Unicode BMP U+0080..U+FFFF (MySQL/MariaDB 10).
	// 8 Capture-Gruppen: User in 1–4 (backtick, double, single, unquoted), Host in 5–8.
	userHostRe = regexp.MustCompile("(?:`([^`]+)`|\"([^\"]+)\"|'([^']+)'|([a-zA-Z0-9$_\\x{80}-\\x{FFFF}]+))\\s*@\\s*(?:`([^`]+)`|\"([^\"]+)\"|'([^']+)'|([a-zA-Z0-9$_\\x{80}-\\x{FFFF}]+))")
	// IDENTIFIED BY PASSWORD mit einem Quote: `...`, "..." oder '...' (müssen matchen)
	identifiedByRe = regexp.MustCompile("(?i)IDENTIFIED\\s+BY\\s+PASSWORD\\s+(?:`([^`]*)`|\"([^\"]*)\"|'([^']*)')")
	// ON dbname.*: DB-Name als `db`, "db", 'db' oder unquoted (ASCII + BMP U+0080..U+FFFF)
	grantOnDbRe = regexp.MustCompile("(?i)ON\\s+(?:`([^`]+)`|\"([^\"]+)\"|'([^']+)'|([a-zA-Z0-9$_\\x{80}-\\x{FFFF}]+))\\s*\\.\\s*\\*")
	// Strip IDENTIFIED BY PASSWORD gefolgt von einem beliebigen Quote-Typ
	stripIdentRe = regexp.MustCompile("(?i)\\s*IDENTIFIED\\s+BY\\s+PASSWORD\\s+(?:`[^`]*`|\"[^\"]*\"|'[^']*')")
)

// extractUserHost returns (user, host) from userHostRe submatch; m[1..4] = user (genau eine gesetzt), m[5..8] = host.
func extractUserHost(m []string) (user, host string) {
	if len(m) < 9 {
		return "", ""
	}
	user = strings.TrimSpace(m[1] + m[2] + m[3] + m[4])
	host = strings.TrimSpace(m[5] + m[6] + m[7] + m[8])
	return user, host
}

// extractIdentPassword returns the password string from identifiedByRe submatch; m[1..3] = backtick, double, single (genau eine gesetzt).
func extractIdentPassword(m []string) string {
	if len(m) < 4 {
		return ""
	}
	return strings.TrimSpace(m[1] + m[2] + m[3])
}

// extractGrantDb returns the database name from grantOnDbRe submatch; m[1..4] = backtick, double, single, unquoted. Leer oder "*" für ON *.*.
func extractGrantDb(m []string) string {
	if len(m) < 5 {
		return ""
	}
	db := strings.TrimSpace(m[1] + m[2] + m[3] + m[4])
	if db == "*" {
		return ""
	}
	return db
}

// grantLine holds one GRANT statement and the db it applies to (empty for ON *.*).
type grantLine struct {
	raw string
	db  string
}

// userRecord holds parsed data for one user (name): hosts, password, grants, dbs.
type userRecord struct {
	name     string
	hosts    []string
	hostSet  map[string]bool
	password string
	pwByHost map[string]string
	grants   []grantLine
	dbs      map[string]bool
}

func newUserRecord(name string) *userRecord {
	return &userRecord{
		name:     name,
		hostSet:  make(map[string]bool),
		pwByHost: make(map[string]string),
		dbs:      make(map[string]bool),
	}
}

func (u *userRecord) addHost(host string) {
	if u.hostSet[host] {
		return
	}
	u.hostSet[host] = true
	u.hosts = append(u.hosts, host)
}

// setPassword sets the password hash for the given host. Returns an error if the same user@host
// already has a different hash (caller should log as warning and keep first).
func (u *userRecord) setPassword(host, hash string) error {
	if hash == "" {
		return nil
	}
	if prev, ok := u.pwByHost[host]; ok && prev != hash {
		return fmt.Errorf(i18n.Tf("err.user_differing_password", u.name, host))
	}
	u.pwByHost[host] = hash
	if u.password == "" {
		u.password = hash
	}
	return nil
}

func (u *userRecord) hasDifferentPasswords() bool {
	if len(u.pwByHost) <= 1 {
		return false
	}
	var first string
	for _, p := range u.pwByHost {
		if first == "" {
			first = p
			continue
		}
		if p != first {
			return true
		}
	}
	return false
}

// parseUserRecords parses the full user SQL into a list of userRecord (by user name).
// CREATE USER adds name+host(+password); GRANT ... TO name@host adds host, db, grant line.
// warn is optional; if set, password conflicts (same user@host, different hash) are logged as warnings.
func parseUserRecords(sql []byte, warn func(string, ...interface{})) map[string]*userRecord {
	users := make(map[string]*userRecord)
	sc := bufio.NewScanner(bytes.NewReader(sql))
	sc.Buffer(nil, 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "CREATE USER ") {
			m := userHostRe.FindStringSubmatch(trimmed)
			name, host := extractUserHost(m)
			if name != "" && host != "" {
				u, ok := users[name]
				if !ok {
					u = newUserRecord(name)
					users[name] = u
				}
				u.addHost(host)
				if subm := identifiedByRe.FindStringSubmatch(trimmed); len(subm) >= 4 {
					if pw := extractIdentPassword(subm); pw != "" {
						if err := u.setPassword(host, pw); err != nil && warn != nil {
							warn("%v", err)
						}
					}
				}
			}
			continue
		}
		if strings.HasPrefix(upper, "GRANT ") {
			m := userHostRe.FindStringSubmatch(trimmed)
			name, host := extractUserHost(m)
			if name == "" || host == "" {
				continue
			}
			u, ok := users[name]
			if !ok {
				u = newUserRecord(name)
				users[name] = u
			}
			u.addHost(host)
			if subm := identifiedByRe.FindStringSubmatch(trimmed); len(subm) >= 4 {
				if pw := extractIdentPassword(subm); pw != "" {
					if err := u.setPassword(host, pw); err != nil && warn != nil {
						warn("%v", err)
					}
				}
			}
			db := ""
			if onDb := grantOnDbRe.FindStringSubmatch(trimmed); len(onDb) >= 5 {
				db = extractGrantDb(onDb)
				if db != "" {
					u.dbs[db] = true
				}
			}
			u.grants = append(u.grants, grantLine{raw: line, db: db})
		}
	}
	return users
}

// ParseUserSQL parses the full user export SQL. For each database, outputs CREATE USER IF NOT EXISTS
// (one per host, same password; warn if passwords differ) and GRANT lines for that db without IDENTIFIED BY.
// warn is optional (e.g. log.Warn); if nil, no warnings are emitted.
// Returns dbToSQL and a list of "user@host" for logging (aus der gleichen Struktur, kein zweites Parsing).
func ParseUserSQL(sql []byte, warn func(string, ...interface{})) (map[string]string, []string) {
	if len(sql) == 0 {
		return map[string]string{}, nil
	}
	users := parseUserRecords(sql, warn)
	userNames := userNamesFromUsers(users)
	dbToSQL := make(map[string]string)
	for _, u := range users {
		if len(u.dbs) == 0 {
			continue
		}
		if u.hasDifferentPasswords() && warn != nil {
			warn(i18n.Tf("log.warn.user_different_passwords", u.name))
		}
		passHash := u.password
		for db := range u.dbs {
			db = strings.TrimSpace(db)
			if db == "" {
				continue
			}
			var block strings.Builder
			for _, h := range u.hosts {
				if passHash != "" {
					block.WriteString("CREATE USER IF NOT EXISTS '")
					block.WriteString(escapeSQL(u.name))
					block.WriteString("'@'")
					block.WriteString(escapeSQL(h))
					block.WriteString("' IDENTIFIED BY PASSWORD '")
					block.WriteString(escapeSQL(passHash))
					block.WriteString("';\n")
				} else {
					block.WriteString("CREATE USER IF NOT EXISTS '")
					block.WriteString(escapeSQL(u.name))
					block.WriteString("'@'")
					block.WriteString(escapeSQL(h))
					block.WriteString("';\n")
				}
			}
			for _, g := range u.grants {
				if g.db != db {
					continue
				}
				stripped := stripIdentRe.ReplaceAllString(g.raw, "")
				stripped = strings.TrimSpace(stripped)
				if stripped != "" {
					if !strings.HasSuffix(stripped, ";") {
						stripped += ";"
					}
					block.WriteString(stripped)
					block.WriteString("\n")
				}
			}
			s := block.String()
			if s == "" {
				continue
			}
			existing := dbToSQL[db]
			if existing != "" {
				existing += "\n\n"
			}
			dbToSQL[db] = existing + strings.TrimRight(s, "\n")
		}
	}
	return dbToSQL, userNames
}

// userNamesFromUsers baut die Liste "user@host" aus der bereits geparsten User-Struktur (kein erneutes Parsing).
func userNamesFromUsers(users map[string]*userRecord) []string {
	seen := make(map[string]bool)
	var names []string
	for _, u := range users {
		for _, h := range u.hosts {
			key := u.name + "@" + h
			if seen[key] {
				continue
			}
			seen[key] = true
			names = append(names, key)
		}
	}
	return names
}

func escapeSQL(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "'", "''")
}


