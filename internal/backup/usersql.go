// Package backup implements MySQL backup: user export parsing, dump, append, zip.
package backup

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// UserBlock is one user's CREATE USER and GRANT statements (as raw SQL lines).
type UserBlock struct {
	SQL    string   // full block including newlines
	DBs    []string // database names from GRANT ... ON `db`.* or db.*
}

var (
	// grantDB matches GRANT ... ON `dbname`.* or dbname.*
	grantDB = regexp.MustCompile("(?i)\\bON\\s+[`]?([^`*.\\s]+)[`]?\\s*\\.\\s*\\*")
)

// ParseUserSQL splits full user export SQL into per-user blocks. For each database, only the
// CREATE USER (rewritten to IF NOT EXISTS) and the GRANTs for that database are appended.
// Users with rights on multiple databases are appended to each matching backup with only the
// relevant GRANTs. Users with only global privileges (e.g. root) have no DBs and are not assigned.
func ParseUserSQL(sql []byte) map[string]string {
	blocks := splitUserBlocks(sql)
	dbToSQL := make(map[string]string)
	for _, b := range blocks {
		createPart, grantsByDB := splitBlockIntoCreateAndGrants(b.SQL)
		if createPart == "" && len(grantsByDB) == 0 {
			continue
		}
		for db, grantLines := range grantsByDB {
			db = strings.TrimSpace(db)
			if db == "" || len(grantLines) == 0 {
				continue
			}
			blockSQL := createPart
			if blockSQL != "" {
				blockSQL += "\n"
			}
			blockSQL += strings.Join(grantLines, "\n")
			existing := dbToSQL[db]
			if existing != "" {
				existing += "\n\n"
			}
			dbToSQL[db] = existing + blockSQL
		}
	}
	return dbToSQL
}

// splitBlockIntoCreateAndGrants splits one user block into CREATE USER part (rewritten to IF NOT EXISTS)
// and GRANT lines grouped by database. Only GRANTs that apply to a specific db (ON `db`.*) are included.
func splitBlockIntoCreateAndGrants(blockSQL string) (createPart string, grantsByDB map[string][]string) {
	grantsByDB = make(map[string][]string)
	var createLines []string
	lines := strings.Split(blockSQL, "\n")
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			i++
			continue
		}
		if strings.HasPrefix(strings.ToUpper(trimmed), "GRANT ") {
			break
		}
		createLines = append(createLines, lines[i])
		i++
	}
	if len(createLines) > 0 {
		createPart = rewriteCreateUserToIfNotExists(strings.Join(createLines, "\n"))
	}
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "GRANT ") {
			if m := grantDB.FindStringSubmatch(trimmed); len(m) >= 2 {
				db := strings.TrimSpace(m[1])
				if db != "" {
					grantsByDB[db] = append(grantsByDB[db], line)
				}
			}
		}
		i++
	}
	return createPart, grantsByDB
}

// rewriteCreateUserToIfNotExists rewrites CREATE USER to CREATE USER IF NOT EXISTS so that
// restore does not fail if the user already exists (e.g. from another restored backup).
// Leaves CREATE USER IF NOT EXISTS and CREATE OR REPLACE USER unchanged.
func rewriteCreateUserToIfNotExists(createPart string) string {
	upper := strings.ToUpper(createPart)
	if strings.Contains(upper, "IF NOT EXISTS") || strings.Contains(upper, "OR REPLACE USER ") {
		return createPart
	}
	// Insert "IF NOT EXISTS " after "CREATE USER " (case insensitive, first occurrence only)
	const createUser = "CREATE USER "
	idx := strings.Index(upper, createUser)
	if idx < 0 {
		return createPart
	}
	return createPart[:idx+len(createUser)] + "IF NOT EXISTS " + createPart[idx+len(createUser):]
}

// splitUserBlocks splits SQL into blocks: each block starts with CREATE USER and includes following GRANTs until next CREATE USER or end.
func splitUserBlocks(sql []byte) []UserBlock {
	var blocks []UserBlock
	sc := bufio.NewScanner(bytes.NewReader(sql))
	sc.Buffer(nil, 1024*1024)
	var current strings.Builder
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "CREATE USER ") {
			if current.Len() > 0 {
				blocks = append(blocks, UserBlock{SQL: strings.TrimRight(current.String(), "\n")})
				current.Reset()
			}
		}
		if current.Len() > 0 || trimmed != "" {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	if current.Len() > 0 {
		blocks = append(blocks, UserBlock{SQL: strings.TrimRight(current.String(), "\n")})
	}
	// Enrich each block with its DB list
	for i := range blocks {
		blocks[i].DBs = extractDBsFromBlock(blocks[i].SQL)
	}
	return blocks
}

func extractDBsFromBlock(sql string) []string {
	seen := make(map[string]bool)
	for _, m := range grantDB.FindAllStringSubmatch(sql, -1) {
		if len(m) >= 2 {
			db := strings.TrimSpace(m[1])
			if db != "" {
				seen[db] = true
			}
		}
	}
	var dbs []string
	for d := range seen {
		dbs = append(dbs, d)
	}
	return dbs
}
