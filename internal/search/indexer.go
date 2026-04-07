// Package search provides full-text search indexing and querying for Crush
// sessions across all project databases.
//
// The indexer reads messages from each project's crush.db, extracts text from
// the JSON parts array, converts Chinese characters to pinyin, and inserts
// into FTS5 tables in a single index database (~/.crush/search.db).
//
// Incremental updates track last_message_rowid per source DB in a meta table.
package search

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
	_ "modernc.org/sqlite"
)

// Project represents a tracked project directory with its data location.
type Project struct {
	Path    string
	DataDir string
}

// part represents one element of the messages.parts JSON array.
// Crush format: {"type":"text","data":{"text":"..."}}
type part struct {
	Type string   `json:"type"`
	Data partData `json:"data"`
}

type partData struct {
	Text string `json:"text"`
}

// IndexDBPath returns the path to the FTS5 index database.
// On Unix: ~/.crush/search.db
// On Windows: %APPDATA%/crush/search.db
func IndexDBPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get config dir: %w", err)
	}
	return filepath.Join(configDir, "crush", "search.db"), nil
}

// openIndexDB opens the index database with WAL mode and creates tables if needed.
func openIndexDB() (*sql.DB, error) {
	dbPath, err := IndexDBPath()
	if err != nil {
		return nil, err
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}

	params := url.Values{}
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(NORMAL)")
	params.Add("_pragma", "busy_timeout(5000)")

	dsn := fmt.Sprintf("file:%s?%s", dbPath, params.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open index db: %w", err)
	}

	if err := createIndexTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create index tables: %w", err)
	}

	return db, nil
}

func createIndexTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS message_fts USING fts5(
			session_id UNINDEXED,
			text_content,
			pinyin_content,
			initials_content,
			tokenize='porter unicode61',
			prefix='2 3'
		);

		CREATE TABLE IF NOT EXISTS message_initials (
			rowid INTEGER PRIMARY KEY,
			session_id TEXT NOT NULL,
			initials TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`)
	return err
}

// UpdateIndex incrementally indexes new messages from all project databases
// into the FTS5 index. Only messages with rowid greater than the last indexed
// rowid are processed.
func UpdateIndex(projects []Project) error {
	crushDBs := discoverDBs(projects)
	if len(crushDBs) == 0 {
		return nil
	}

	idxConn, err := openIndexDB()
	if err != nil {
		return err
	}
	defer idxConn.Close()

	pinyinArgs := pinyin.NewArgs()
	pinyinArgs.Style = pinyin.Normal

	for _, dbPath := range crushDBs {
		if err := indexOneDB(idxConn, dbPath, pinyinArgs); err != nil {
			// Log but continue with other DBs.
			continue
		}
	}

	return nil
}

// RebuildIndex clears the entire FTS5 index and re-indexes all messages
// from all project databases.
func RebuildIndex(projects []Project) error {
	crushDBs := discoverDBs(projects)
	if len(crushDBs) == 0 {
		return nil
	}

	idxConn, err := openIndexDB()
	if err != nil {
		return err
	}
	defer idxConn.Close()

	// Clear all index data.
	if _, err := idxConn.Exec("DELETE FROM message_fts"); err != nil {
		return fmt.Errorf("rebuild clear fts: %w", err)
	}
	if _, err := idxConn.Exec("DELETE FROM message_initials"); err != nil {
		return fmt.Errorf("rebuild clear initials: %w", err)
	}
	if _, err := idxConn.Exec("DELETE FROM meta"); err != nil {
		return fmt.Errorf("rebuild clear meta: %w", err)
	}

	pinyinArgs := pinyin.NewArgs()
	pinyinArgs.Style = pinyin.Normal

	for _, dbPath := range crushDBs {
		if err := indexOneDB(idxConn, dbPath, pinyinArgs); err != nil {
			continue
		}
	}

	return nil
}

// discoverDBs returns paths to existing crush.db files across all projects.
func discoverDBs(projects []Project) []string {
	seen := make(map[string]bool)
	var dbs []string
	for _, p := range projects {
		db := filepath.Join(p.DataDir, "crush.db")
		if seen[db] {
			continue
		}
		if _, err := os.Stat(db); err == nil {
			dbs = append(dbs, db)
			seen[db] = true
		}
	}
	return dbs
}

func indexOneDB(idxConn *sql.DB, crushDBPath string, pinyinArgs pinyin.Args) error {
	metaKey := "last_rowid:" + crushDBPath

	params := url.Values{}
	params.Set("mode", "ro")
	params.Add("_pragma", "journal_mode(WAL)")

	dsn := fmt.Sprintf("file:%s?%s", crushDBPath, params.Encode())
	srcConn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", crushDBPath, err)
	}
	defer srcConn.Close()

	lastRowID := getLastRowID(idxConn, metaKey)

	// Build parent lookup: child session -> root parent session.
	parentMap := make(map[string]string)
	prows, err := srcConn.Query(`
		SELECT id, parent_session_id FROM sessions
		WHERE parent_session_id IS NOT NULL AND parent_session_id != ''
	`)
	if err != nil {
		return fmt.Errorf("query parents %s: %w", crushDBPath, err)
	}
	for prows.Next() {
		var id, parent string
		if err := prows.Scan(&id, &parent); err != nil {
			continue
		}
		parentMap[id] = parent
	}
	prows.Close()

	rows, err := srcConn.Query(`
		SELECT rowid, session_id, role, parts
		FROM messages
		WHERE rowid > ?
		ORDER BY rowid ASC
	`, lastRowID)
	if err != nil {
		return fmt.Errorf("query messages %s: %w", crushDBPath, err)
	}
	defer rows.Close()

	tx, err := idxConn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	insertFTS, err := tx.Prepare(`
		INSERT INTO message_fts (session_id, text_content, pinyin_content, initials_content)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare fts: %w", err)
	}
	defer insertFTS.Close()

	insertInitials, err := tx.Prepare(`
		INSERT INTO message_initials (session_id, initials)
		VALUES (?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare initials: %w", err)
	}
	defer insertInitials.Close()

	var maxRowID int64

	for rows.Next() {
		var rowid int64
		var sessionID, role, partsJSON string
		if err := rows.Scan(&rowid, &sessionID, &role, &partsJSON); err != nil {
			continue
		}

		if role != "user" && role != "assistant" {
			maxRowID = rowid
			continue
		}

		indexID := sessionID
		if parent, ok := parentMap[sessionID]; ok {
			indexID = parent
		}

		text := extractText(partsJSON)
		if text == "" {
			maxRowID = rowid
			continue
		}

		py, initials := toPinyinAndInitials(text, pinyinArgs)

		if _, err := insertFTS.Exec(indexID, text, py, initials); err != nil {
			continue
		}
		if _, err := insertInitials.Exec(indexID, initials); err != nil {
			continue
		}

		maxRowID = rowid
	}
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return fmt.Errorf("rows error %s: %w", crushDBPath, err)
	}

	if maxRowID > lastRowID {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`,
			metaKey, fmt.Sprintf("%d", maxRowID),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("update meta: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func getLastRowID(db *sql.DB, metaKey string) int64 {
	var val sql.NullString
	_ = db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey).Scan(&val)
	if !val.Valid {
		return 0
	}
	var id int64
	fmt.Sscanf(val.String, "%d", &id)
	return id
}

func extractText(partsJSON string) string {
	var parts []json.RawMessage
	if err := json.Unmarshal([]byte(partsJSON), &parts); err != nil {
		return ""
	}

	var texts []string
	for _, raw := range parts {
		var p part
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if p.Data.Text != "" {
			texts = append(texts, p.Data.Text)
		}
	}
	return strings.Join(texts, " ")
}

// ToPinyinAndInitials converts Chinese characters in text to pinyin and
// extracts the initial letter of each syllable. Non-Han characters are
// passed through unchanged in the pinyin output.
func ToPinyinAndInitials(text string) (string, string) {
	args := pinyin.NewArgs()
	args.Style = pinyin.Normal
	return toPinyinAndInitials(text, args)
}

func toPinyinAndInitials(text string, args pinyin.Args) (string, string) {
	var pyParts []string
	var initials []byte

	runes := []rune(text)
	i := 0
	lastWasHan := false
	for i < len(runes) {
		r := runes[i]
		if unicode.Is(unicode.Han, r) {
			start := i
			for i < len(runes) && unicode.Is(unicode.Han, runes[i]) {
				i++
			}
			hanStr := string(runes[start:i])
			pyResult := pinyin.Pinyin(hanStr, args)
			for _, syllable := range pyResult {
				if len(syllable) > 0 && len(syllable[0]) > 0 {
					pyParts = append(pyParts, syllable[0])
					initials = append(initials, syllable[0][0])
				}
			}
			lastWasHan = true
		} else {
			start := i
			for i < len(runes) && !unicode.Is(unicode.Han, runes[i]) {
				i++
			}
			chunk := string(runes[start:i])
			pyParts = append(pyParts, chunk)
			// Insert a space separator in initials so that matches
			// don't span across punctuation/whitespace boundaries.
			if lastWasHan {
				initials = append(initials, ' ')
			}
			lastWasHan = false
		}
	}

	return strings.Join(pyParts, " "), string(initials)
}
