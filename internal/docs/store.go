package docs

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

const (
	indexRefreshInterval = 30 * 24 * time.Hour
	maxDocumentSize      = 1 << 20
	defaultResultLimit   = 5
	maxResultLimit       = 10
	maxQueryTerms        = 12
	maxTokenRunes        = 80
	maxSnippetRunes      = 360
)

var manPageName = regexp.MustCompile(`\.[0-9][[:alnum:]]*$`)
var manDirectory = regexp.MustCompile(`^man[0-9][[:alnum:]]*$`)
var formattingEscape = regexp.MustCompile(`\\f(\[[^]]+\]|.)`)

type Store struct {
	db      *sql.DB
	release Release
}

type Result struct {
	DistroID   string
	Title      string
	Category   string
	SourcePath string
	Snippet    string
}

type sourceFile struct {
	path     string
	category string
	manPage  bool
}

func DefaultPath() (string, error) {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataDir, "linux-mentor", "docs.db"), nil
}

func OpenDefault(ctx context.Context) (*Store, error) {
	release, err := ReadRelease("/etc/os-release")
	if err != nil {
		return nil, err
	}
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Open(ctx, path, release, DefaultSourceRoots())
}

func DefaultSourceRoots() []string {
	roots := []string{
		"/usr/share/man",
		"/usr/local/share/man",
		"/usr/man",
		"/usr/local/man",
		"/usr/share/doc",
		"/usr/local/share/doc",
	}
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		seen[root] = true
	}
	for _, root := range filepath.SplitList(os.Getenv("MANPATH")) {
		if root != "" && !seen[root] {
			roots = append(roots, root)
			seen[root] = true
		}
	}
	return roots
}

func Open(ctx context.Context, path string, release Release, roots []string) (*Store, error) {
	if release.ID == "" {
		return nil, errors.New("distro ID is empty")
	}
	if path == "" {
		return nil, errors.New("documentation database path is empty")
	}
	if path != ":memory:" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve documentation database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
			return nil, fmt.Errorf("create documentation database directory: %w", err)
		}
		if err := os.Chmod(filepath.Dir(absPath), 0o700); err != nil {
			return nil, fmt.Errorf("secure documentation database directory: %w", err)
		}
		path = absPath
	}

	dsn := path
	if path != ":memory:" {
		dsn = (&url.URL{Scheme: "file", Path: path}).String() +
			"?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open documentation SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db, release: release}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to documentation SQLite database: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if path != ":memory:" {
		if err := os.Chmod(path, 0o600); err != nil {
			db.Close()
			return nil, fmt.Errorf("secure documentation database permissions: %w", err)
		}
	}
	if err := store.indexIfNeeded(ctx, roots, time.Now()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DistroID() string {
	return s.release.ID
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY,
			distro_id TEXT NOT NULL,
			title TEXT NOT NULL,
			category TEXT NOT NULL,
			source_path TEXT NOT NULL,
			content TEXT NOT NULL,
			indexed_at TEXT NOT NULL,
			UNIQUE(distro_id, source_path)
		);
		CREATE INDEX IF NOT EXISTS documents_distro_idx ON documents(distro_id);
		CREATE TABLE IF NOT EXISTS document_terms (
			document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			token TEXT NOT NULL,
			frequency INTEGER NOT NULL CHECK(frequency > 0),
			PRIMARY KEY(document_id, token)
		);
		CREATE INDEX IF NOT EXISTS document_terms_lookup_idx
			ON document_terms(token, document_id);
		CREATE TABLE IF NOT EXISTS index_state (
			distro_id TEXT PRIMARY KEY,
			indexed_at TEXT NOT NULL,
			document_count INTEGER NOT NULL CHECK(document_count >= 0)
		);
	`)
	if err != nil {
		return fmt.Errorf("create documentation SQLite schema: %w", err)
	}
	return nil
}

func (s *Store) indexIfNeeded(ctx context.Context, roots []string, now time.Time) error {
	var indexedAt string
	var documentCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT indexed_at, document_count FROM index_state WHERE distro_id = ?
	`, s.release.ID).Scan(&indexedAt, &documentCount)
	if err == nil && documentCount > 0 {
		previous, parseErr := time.Parse(time.RFC3339Nano, indexedAt)
		if parseErr != nil {
			return fmt.Errorf("parse documentation index time: %w", parseErr)
		}
		if now.Sub(previous) < indexRefreshInterval {
			return nil
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read documentation index state: %w", err)
	}

	files, err := collectSourceFiles(roots)
	if err != nil {
		return err
	}
	documents := make([]indexedDocument, 0, len(files))
	for _, file := range files {
		document, err := readDocument(file)
		if err != nil {
			return err
		}
		if document != nil && len(document.terms) > 0 {
			documents = append(documents, *document)
		}
	}
	if err := s.replaceDistroIndex(ctx, documents, now); err != nil {
		return err
	}
	return nil
}

type indexedDocument struct {
	sourceFile
	title   string
	content string
	terms   map[string]int
}

func collectSourceFiles(roots []string) ([]sourceFile, error) {
	found := make(map[string]sourceFile)
	for _, root := range roots {
		rootInfo, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect documentation root %q: %w", root, err)
		}
		if !rootInfo.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk documentation path %q: %w", path, walkErr)
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("resolve documentation path %q: %w", path, err)
			}
			category, isManPage, eligible := classifyFile(relative)
			if eligible {
				found[path] = sourceFile{path: path, category: category, manPage: isManPage}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	files := make([]sourceFile, 0, len(found))
	for _, file := range found {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func classifyFile(relative string) (category string, manPage, eligible bool) {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 2 && manDirectory.MatchString(parts[0]) {
		name := strings.TrimSuffix(parts[1], ".gz")
		if manPageName.MatchString(name) {
			return parts[0], true, true
		}
		return "", false, false
	}
	if len(parts) < 2 {
		return "", false, false
	}
	name := strings.ToLower(filepath.Base(relative))
	compressed := strings.HasSuffix(name, ".gz")
	if compressed {
		name = strings.TrimSuffix(name, ".gz")
	}
	extension := strings.ToLower(filepath.Ext(name))
	documentName := extension == ".md" || extension == ".txt" || extension == ".rst" ||
		name == "readme" || strings.HasPrefix(name, "readme.") ||
		name == "news" || strings.HasPrefix(name, "news.")
	if documentName {
		return "package-doc", false, true
	}
	return "", false, false
}

func readDocument(file sourceFile) (*indexedDocument, error) {
	info, err := os.Stat(file.path)
	if err != nil {
		return nil, fmt.Errorf("stat local documentation %q: %w", file.path, err)
	}
	if info.Size() > maxDocumentSize {
		return nil, nil
	}
	raw, err := readLocalText(file.path)
	if err != nil {
		return nil, fmt.Errorf("read local documentation %q: %w", file.path, err)
	}
	if len(raw) == 0 || len(raw) > maxDocumentSize || bytes.IndexByte(raw, 0) >= 0 {
		return nil, nil
	}
	content := strings.ToValidUTF8(string(raw), " ")
	if file.manPage {
		content = cleanRoff(content)
	} else {
		content = strings.Join(strings.Fields(content), " ")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	return &indexedDocument{
		sourceFile: file,
		title:      documentTitle(file, string(raw)),
		content:    content,
		terms:      termFrequencies(content),
	}, nil
}

func readLocalText(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	limited := io.LimitReader(reader, maxDocumentSize+1)
	return io.ReadAll(limited)
}

func documentTitle(file sourceFile, content string) string {
	name := filepath.Base(file.path)
	if strings.HasSuffix(strings.ToLower(name), ".gz") {
		name = name[:len(name)-3]
	}
	if file.manPage {
		if title := manTitle(content); title != "" {
			return title
		}
		name = manPageName.ReplaceAllString(name, "")
	}
	return strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
}

func manTitle(content string) string {
	for _, line := range strings.SplitN(content, "\n", 12) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == ".TH" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

func cleanRoff(content string) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ".\\\"") {
			continue
		}
		if strings.HasPrefix(trimmed, ".SH ") || strings.HasPrefix(trimmed, ".SS ") {
			lines = append(lines, strings.Trim(strings.TrimSpace(trimmed[4:]), `"`))
			continue
		}
		if strings.HasPrefix(trimmed, ".TH ") || strings.HasPrefix(trimmed, ".Dt ") ||
			strings.HasPrefix(trimmed, ".Dd ") || strings.HasPrefix(trimmed, ".Os ") {
			continue
		}
		if strings.HasPrefix(trimmed, ".") || strings.HasPrefix(trimmed, "'") {
			fields := strings.Fields(trimmed)
			if len(fields) < 2 || !isTextRoffMacro(fields[0]) {
				continue
			}
			trimmed = strings.Join(fields[1:], " ")
		}
		lines = append(lines, trimmed)
	}
	text := strings.Join(lines, " ")
	text = formattingEscape.ReplaceAllString(text, "")
	return strings.NewReplacer(
		`\\`, `\`,
		`\-`, "-",
		`\&`, "",
		`\%`, "",
		`\~`, " ",
		`\(bu`, "*",
		`\(em`, "--",
		`\(en`, "-",
		`\(aq`, "'",
		`\(lq`, `"`,
		`\(rq`, `"`,
	).Replace(text)
}

func isTextRoffMacro(macro string) bool {
	switch macro {
	case ".B", ".I", ".BR", ".BI", ".IR", ".RB", ".RI", ".SM", ".SB", ".LP":
		return true
	default:
		return false
	}
}

func termFrequencies(text string) map[string]int {
	terms := make(map[string]int)
	var word []rune
	flush := func() {
		if len(word) >= 2 && len(word) <= maxTokenRunes {
			terms[string(word)]++
		}
		word = word[:0]
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return terms
}

func (s *Store) replaceDistroIndex(ctx context.Context, documents []indexedDocument, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local documentation index transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE distro_id = ?`, s.release.ID); err != nil {
		return fmt.Errorf("replace documentation section for %q: %w", s.release.ID, err)
	}
	documentStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO documents(distro_id, title, category, source_path, content, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare documentation insert: %w", err)
	}
	defer documentStmt.Close()
	termStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO document_terms(document_id, token, frequency) VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare documentation term insert: %w", err)
	}
	defer termStmt.Close()

	indexedAt := now.UTC().Format(time.RFC3339Nano)
	for _, document := range documents {
		result, err := documentStmt.ExecContext(ctx, s.release.ID, document.title,
			document.category, document.path, document.content, indexedAt)
		if err != nil {
			return fmt.Errorf("index documentation file %q: %w", document.path, err)
		}
		documentID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("get documentation ID for %q: %w", document.path, err)
		}
		for token, frequency := range document.terms {
			if _, err := termStmt.ExecContext(ctx, documentID, token, frequency); err != nil {
				return fmt.Errorf("index term in %q: %w", document.path, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO index_state(distro_id, indexed_at, document_count) VALUES (?, ?, ?)
		ON CONFLICT(distro_id) DO UPDATE SET
			indexed_at = excluded.indexed_at,
			document_count = excluded.document_count
	`, s.release.ID, indexedAt, len(documents)); err != nil {
		return fmt.Errorf("save documentation index state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local documentation index: %w", err)
	}
	return nil
}

func (s *Store) DocumentCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM documents WHERE distro_id = ?
	`, s.release.ID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count indexed documentation: %w", err)
	}
	return count, nil
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return []Result{}, nil
	}
	if limit < 1 || limit > maxResultLimit {
		limit = defaultResultLimit
	}
	documentCount, err := s.DocumentCount(ctx)
	if err != nil {
		return nil, err
	}
	if documentCount == 0 {
		return []Result{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tokens)), ",")
	args := make([]any, 0, len(tokens)+3)
	args = append(args, s.release.ID, s.release.ID)
	for _, token := range tokens {
		args = append(args, token)
	}
	args = append(args, limit)

	querySQL := `
		WITH document_frequency AS (
			SELECT terms.token, COUNT(*) AS frequency
			FROM document_terms terms
			JOIN documents docs ON docs.id = terms.document_id
			WHERE docs.distro_id = ?
			GROUP BY terms.token
		),
		ranked AS (
			SELECT docs.id,
				-- Rare terms weigh more; repeated matches are capped.
				SUM(1.0 * MIN(terms.frequency, 8) / document_frequency.frequency) AS score,
				COUNT(DISTINCT terms.token) AS matched_terms
			FROM documents docs
			JOIN document_terms terms ON terms.document_id = docs.id
			JOIN document_frequency ON document_frequency.token = terms.token
			WHERE docs.distro_id = ? AND terms.token IN (` + placeholders + `)
			GROUP BY docs.id
			ORDER BY score DESC, matched_terms DESC
			LIMIT ?
		)
		SELECT docs.title, docs.category, docs.source_path, docs.content
		FROM ranked
		JOIN documents docs ON docs.id = ranked.id
		ORDER BY ranked.score DESC, ranked.matched_terms DESC, docs.title
	`
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("search local %q documentation: %w", s.release.ID, err)
	}
	defer rows.Close()

	results := make([]Result, 0, limit)
	for rows.Next() {
		var title, category, sourcePath, content string
		if err := rows.Scan(&title, &category, &sourcePath, &content); err != nil {
			return nil, fmt.Errorf("read documentation search result: %w", err)
		}
		results = append(results, Result{
			DistroID:   s.release.ID,
			Title:      title,
			Category:   category,
			SourcePath: sourcePath,
			Snippet:    excerpt(content, tokens, maxSnippetRunes),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documentation search results: %w", err)
	}
	return results, nil
}

func queryTokens(query string) []string {
	frequencies := termFrequencies(query)
	tokens := make([]string, 0, len(frequencies))
	for token := range frequencies {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	if len(tokens) > maxQueryTerms {
		tokens = tokens[:maxQueryTerms]
	}
	return tokens
}

func excerpt(content string, terms []string, maxRunes int) string {
	runes := []rune(content)
	lower := make([]rune, len(runes))
	for i, r := range runes {
		lower[i] = unicode.ToLower(r)
	}
	start := len(runes)
	for _, term := range terms {
		termRunes := []rune(term)
		for i := 0; i+len(termRunes) <= len(lower); i++ {
			if equalRunes(lower[i:i+len(termRunes)], termRunes) && i < start {
				start = i
			}
		}
	}
	if start == len(runes) {
		start = 0
	}
	start = max(0, start-maxRunes/4)
	end := min(len(runes), start+maxRunes)
	snippet := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		snippet = "… " + snippet
	}
	if end < len(runes) {
		snippet += " …"
	}
	return snippet
}

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
