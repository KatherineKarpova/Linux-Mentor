package docs

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalIndexSearchIsRestrictedToTheDetectedDistro(t *testing.T) {
	ctx := context.Background()
	dataHome := t.TempDir()
	dbPath := filepath.Join(dataHome, "linux-mentor", "docs.db")
	ubuntuRoots := writeDistroDocs(t, dataHome, "ubuntu", `.TH "CHMOD" "1"
.SH NAME
chmod - change file mode
.SH DESCRIPTION
Ubuntu chmod changes file permissions and access modes for a file.
`)
	fedoraRoots := writeDistroDocs(t, dataHome, "fedora", `.TH "CHMOD" "1"
.SH NAME
chmod - change file mode
.SH DESCRIPTION
Fedora chmod changes file permissions and access modes for a file.
`)

	ubuntu, err := Open(ctx, dbPath, Release{ID: "ubuntu"}, ubuntuRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer ubuntu.Close()
	fedora, err := Open(ctx, dbPath, Release{ID: "fedora"}, fedoraRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer fedora.Close()

	ubuntuResults, err := ubuntu.Search(ctx, "chmod file permissions", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ubuntuResults) == 0 {
		t.Fatal("expected results in Ubuntu's local documentation section")
	}
	for _, result := range ubuntuResults {
		if result.DistroID != "ubuntu" || !strings.Contains(result.SourcePath, string(filepath.Separator)+"ubuntu"+string(filepath.Separator)) {
			t.Errorf("Ubuntu search returned cross-distro result: %+v", result)
		}
	}

	fedoraResults, err := fedora.Search(ctx, "chmod file permissions", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(fedoraResults) == 0 {
		t.Fatal("expected results in Fedora's local documentation section")
	}
	for _, result := range fedoraResults {
		if result.DistroID != "fedora" || !strings.Contains(result.SourcePath, string(filepath.Separator)+"fedora"+string(filepath.Separator)) {
			t.Errorf("Fedora search returned cross-distro result: %+v", result)
		}
	}

	unknown, err := Open(ctx, dbPath, Release{ID: "unknown-distro"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Close()
	results, err := unknown.Search(ctx, "chmod file permissions", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("unsupported distro searched another section: %+v", results)
	}
}

func TestIndexIncludesTextDocsAndKeepsDatabasePrivate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsRoot := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docsRoot, "example"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "example", "README.md"),
		[]byte("# Package notes\n\nA unique diagnostic phrase for the local documentation index."), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "data", "docs.db")
	store, err := Open(ctx, path, Release{ID: "sample"}, []string{docsRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.Search(ctx, "unique diagnostic phrase", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Category != "package-doc" ||
		results[0].SourcePath != filepath.Join(docsRoot, "example", "README.md") {
		t.Fatalf("search results = %+v", results)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("database permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestReadLocalTextExpandsGzipManPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.1.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte("manual page text")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := readLocalText(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "manual page text" {
		t.Fatalf("expanded text = %q", data)
	}
}

func writeDistroDocs(t *testing.T, root, distro, manContent string) []string {
	t.Helper()
	manRoot := filepath.Join(root, distro, "man")
	docsRoot := filepath.Join(root, distro, "doc")
	if err := os.MkdirAll(filepath.Join(manRoot, "man1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docsRoot, "coreutils"), 0o700); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(manRoot, "man1", "chmod.1.gz")
	file, err := os.Create(manPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte(manContent)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(docsRoot, "coreutils", "README.txt")
	if err := os.WriteFile(readme, []byte(distro+" local docs about command permissions"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{manRoot, docsRoot}
}
