package search

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SearchResult represents a session found by search.
type SearchResult struct {
	DBPath         string
	SessionID      string
	Title          string
	Date           string // formatted date like "04-06"
	MessageCount   string // like "12 msgs"
	ProjectPath    string // display path (~ shortened)
	AbsProjectPath string // absolute path for comparisons
	UpdatedAt      int64  // for sorting (milliseconds since epoch)
	Active         bool   // true if this session is open in a mux window
	Pinyin         string // pinyin representation of title
	Initials       string // pinyin initials of title
}

// Search searches across all project databases for sessions matching the query.
// Empty query returns all sessions sorted by updated_at DESC.
// Non-empty query tokenizes and searches: FTS5 MATCH on message text/pinyin,
// LIKE on message initials, plus Go-side title matching (substring, pinyin,
// initials). Multiple tokens are intersected. Results are merged and deduped.
func Search(projects []Project, query string) ([]SearchResult, error) {
	indexPath, err := IndexDBPath()
	if err != nil {
		return nil, err
	}

	if query == "" {
		var allResults []SearchResult
		for _, p := range projects {
			dbPath := filepath.Join(p.DataDir, "crush.db")
			if _, err := os.Stat(dbPath); err != nil {
				continue
			}
			results, err := searchAllSessions(dbPath, p.Path)
			if err != nil {
				continue
			}
			allResults = append(allResults, results...)
		}
		return allResults, nil
	}

	tokens := TokenizeQuery(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Collect all sessions and FTS5 hits per project, merge with title matching.
	seen := make(map[string]bool)
	var allResults []SearchResult

	for _, p := range projects {
		dbPath := filepath.Join(p.DataDir, "crush.db")
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}

		// FTS5 message content search
		ftsResults, _ := searchWithQuery(dbPath, indexPath, p.Path, tokens)
		for _, r := range ftsResults {
			if !seen[r.SessionID] {
				seen[r.SessionID] = true
				allResults = append(allResults, r)
			}
		}

		// Title matching (substring + pinyin + initials) in Go
		allSessions, err := searchAllSessions(dbPath, p.Path)
		if err != nil {
			continue
		}
		for _, r := range allSessions {
			if seen[r.SessionID] {
				continue
			}
			if titleMatchesAllTokens(r.Title, tokens) {
				seen[r.SessionID] = true
				allResults = append(allResults, r)
			}
		}
	}

	return allResults, nil
}

// SortResults sorts results with active sessions first, then by updated_at DESC.
func SortResults(results []SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Active != results[j].Active {
			return results[i].Active
		}
		return results[i].UpdatedAt > results[j].UpdatedAt
	})
}

// titleMatchesAllTokens checks if a title matches ALL tokens via substring,
// pinyin, or initials matching.
func titleMatchesAllTokens(title string, tokens []string) bool {
	titleLower := strings.ToLower(title)
	py, initials := ToPinyinAndInitials(title)
	pyLower := strings.ToLower(py)
	initialsLower := strings.ToLower(initials)

	for _, token := range tokens {
		tokenLower := strings.ToLower(token)
		if !strings.Contains(titleLower, tokenLower) &&
			!strings.Contains(pyLower, tokenLower) &&
			!strings.Contains(initialsLower, tokenLower) {
			return false
		}
	}
	return true
}

// searchAllSessions returns all root sessions from a crush.db.
func searchAllSessions(dbPath, projectPath string) ([]SearchResult, error) {
	conn, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Shorten home dir in project path for display.
	displayPath := shortenHome(projectPath)

	rows, err := conn.Query(`
		SELECT id, title,
			strftime('%m-%d', updated_at / 1000, 'unixepoch', 'localtime'),
			message_count || ' msgs',
			updated_at
		FROM sessions
		WHERE title IS NOT NULL AND title != ''
			AND parent_session_id IS NULL
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Title, &r.Date, &r.MessageCount, &r.UpdatedAt); err != nil {
			continue
		}
		r.DBPath = dbPath
		r.ProjectPath = displayPath
		r.AbsProjectPath = projectPath
		results = append(results, r)
	}
	return results, rows.Err()
}

// searchWithQuery performs FTS5 search using the index DB.
func searchWithQuery(dbPath, indexPath, projectPath string, tokens []string) ([]SearchResult, error) {
	conn, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Attach the index database.
	_, err = conn.Exec(fmt.Sprintf("ATTACH DATABASE %q AS idx", indexPath))
	if err != nil {
		return nil, fmt.Errorf("attach index db: %w", err)
	}
	defer conn.Exec("DETACH DATABASE idx") //nolint:errcheck

	displayPath := shortenHome(projectPath)

	// Build CTEs for each token, then intersect.
	var ctes []string
	var args []interface{}

	for i, token := range tokens {
		n := i + 1
		// FTS5 MATCH token: quote it and add prefix wildcard.
		ftsToken := `"` + token + `"*`
		// LIKE token: strip spaces for initials matching.
		likeToken := "%" + strings.ReplaceAll(token, " ", "") + "%"
		titleLike := "%" + token + "%"

		cte := fmt.Sprintf(`tok%d AS (
			SELECT DISTINCT session_id FROM idx.message_fts
				WHERE text_content MATCH ?
			UNION
			SELECT DISTINCT session_id FROM idx.message_fts
				WHERE pinyin_content MATCH ?
			UNION
			SELECT DISTINCT session_id FROM idx.message_initials
				WHERE initials LIKE ?
			UNION
			SELECT DISTINCT id AS session_id FROM sessions
				WHERE title LIKE ?
		)`, n)
		ctes = append(ctes, cte)
		args = append(args, ftsToken, ftsToken, likeToken, titleLike)
	}

	// Build intersection of all token CTEs.
	var intersects []string
	for i := range tokens {
		intersects = append(intersects, fmt.Sprintf("SELECT session_id FROM tok%d", i+1))
	}

	sqlQuery := fmt.Sprintf(`
		WITH %s,
		all_matches AS (
			%s
		)
		SELECT s.id, s.title,
			strftime('%%m-%%d', s.updated_at / 1000, 'unixepoch', 'localtime'),
			s.message_count || ' msgs',
			s.updated_at
		FROM sessions s
		JOIN all_matches m ON s.id = m.session_id
		WHERE s.title IS NOT NULL AND s.title != ''
			AND s.parent_session_id IS NULL
		ORDER BY s.updated_at DESC
	`, strings.Join(ctes, ",\n"), strings.Join(intersects, "\nINTERSECT\n"))

	rows, err := conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Title, &r.Date, &r.MessageCount, &r.UpdatedAt); err != nil {
			continue
		}
		r.DBPath = dbPath
		r.ProjectPath = displayPath
		r.AbsProjectPath = projectPath
		results = append(results, r)
	}
	return results, rows.Err()
}

// MarkActive marks sessions that are currently open in mux windows.
// It queries the multiplexer for active session IDs and sets the Active
// flag on matching results.
func MarkActive(results []SearchResult, activeSessionIDs []string) {
	active := make(map[string]bool, len(activeSessionIDs))
	for _, id := range activeSessionIDs {
		if id != "" {
			active[id] = true
		}
	}
	for i := range results {
		if active[results[i].SessionID] {
			results[i].Active = true
		}
	}
}

// Preview returns a message summary for a given session.
// Each line is prefixed with "▶ " for user messages and "◀ " for assistant messages.
// Newlines within messages are replaced with spaces.
func Preview(dbPath, sessionID string) ([]string, error) {
	conn, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT CASE m.role
				WHEN 'user' THEN '▶ '
				WHEN 'assistant' THEN '◀ '
				ELSE '  '
			END ||
			substr(
				replace(
					group_concat(
						json_extract(j.value, '$.data.text'), ''
					),
					char(10), ' '
				), 1, 2000
			)
		FROM messages m,
			json_each(m.parts) j
		WHERE (m.session_id = ?
				OR m.session_id IN (SELECT id FROM sessions WHERE parent_session_id = ?))
			AND json_extract(j.value, '$.type') = 'text'
			AND json_extract(j.value, '$.data.text') IS NOT NULL
		GROUP BY m.id
		ORDER BY m.created_at ASC
	`, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("preview query: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			continue
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

// DeleteSession deletes a session and its associated messages and files
// directly from the specified database. This is used by the session search
// dialog to delete sessions from any project, not just the current workspace.
func DeleteSession(dbPath, sessionID string) error {
	conn, err := openReadWrite(dbPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("deleting session messages: %w", err)
	}
	if _, err = tx.Exec("DELETE FROM files WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("deleting session files: %w", err)
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return tx.Commit()
}

// openReadWrite opens a crush.db in read-write mode with WAL journal.
func openReadWrite(dbPath string) (*sql.DB, error) {
	params := url.Values{}
	params.Set("mode", "rwc")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "busy_timeout(5000)")

	dsn := fmt.Sprintf("file:%s?%s", dbPath, params.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	return db, nil
}

// openReadOnly opens a crush.db in read-only mode with WAL journal.
func openReadOnly(dbPath string) (*sql.DB, error) {
	params := url.Values{}
	params.Set("mode", "ro")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "busy_timeout(5000)")

	dsn := fmt.Sprintf("file:%s?%s", dbPath, params.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	return db, nil
}

// TokenizeQuery splits a query string into sanitized tokens.
// Special characters used by FTS5 are stripped.
func TokenizeQuery(query string) []string {
	var tokens []string
	for _, word := range strings.Fields(query) {
		// Strip FTS5 special characters.
		cleaned := strings.Map(func(r rune) rune {
			switch r {
			case '"', '*', '^', '{', '}', '(', ')', ':':
				return -1
			default:
				return r
			}
		}, word)
		if cleaned != "" {
			tokens = append(tokens, cleaned)
		}
	}
	return tokens
}

// shortenHome replaces the home directory prefix with ~ for display.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

