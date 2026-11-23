package docs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadReleaseUsesExactDistroID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(path, []byte(
		`NAME="Example Linux"`+"\n"+
			`ID=example`+"\n"+
			`ID_LIKE="debian ubuntu"`+"\n"+
			`PRETTY_NAME="Example Linux 1.0"`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	release, err := ReadRelease(path)
	if err != nil {
		t.Fatal(err)
	}
	if release.ID != "example" || release.PrettyName != "Example Linux 1.0" {
		t.Fatalf("release = %+v", release)
	}
}

func TestReadReleaseRequiresAValidID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(path, []byte(`ID_LIKE="debian"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRelease(path); err == nil {
		t.Fatal("expected an error when os-release has no ID")
	}
}
