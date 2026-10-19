package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsAndSources(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "x.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("database mode = %v, want 0600", fi.Mode().Perm())
	}
	a := &Agent{Name: "fs01", URL: "https://fs01:9443", Token: "segredo"}
	if err := s.SaveAgent(a); err != nil || a.ID == 0 {
		t.Fatalf("SaveAgent: %v id=%d", err, a.ID)
	}
	a.URL, a.Token = "https://fs01.empresa.net:9443", "" // an empty token keeps the previous one
	if err := s.SaveAgent(a); err != nil {
		t.Fatal(err)
	}
	got, err := s.Agent(a.ID)
	if err != nil || got.Token != "segredo" || got.URL != a.URL {
		t.Fatalf("Agent = %+v, %v", got, err)
	}
	if err := s.SaveAgent(&Agent{Name: "fs01", URL: "x", Token: "y"}); err == nil {
		t.Error("a duplicate name should fail")
	}
	if err := s.DeleteAgent(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Agent(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	if err := s.SaveSource(&Source{Name: "legado", URL: "ldap://192.168.10.91", Domain: "empresa.net", User: "administrator"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSource(&Source{Name: "legado", URL: "ldaps://x", Domain: "empresa.net"}); err != nil {
		t.Fatal(err)
	}
	src, _ := s.Sources()
	if len(src) != 1 || src[0].URL != "ldaps://x" {
		t.Fatalf("Sources = %+v", src)
	}
}

func TestOpenAppliesStagedRestore(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ad-manager.db")
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.VacuumInto(p + RestoreSuffix); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := os.Stat(p + RestoreSuffix); !os.IsNotExist(err) {
		t.Fatalf("staged database left: %v", err)
	}
	if _, err := os.Stat(p + ".before-restore"); err != nil {
		t.Fatalf("previous database not kept: %v", err)
	}
}

// A staged file that is not a panel database is set aside; the current database stays.
func TestOpenRefusesBadStagedRestore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ad-manager.db")
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	os.WriteFile(p+RestoreSuffix, append([]byte("SQLite format 3\x00"), make([]byte, 100)...), 0o600)
	st, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := os.Stat(p + ".restore-failed"); err != nil {
		t.Fatalf("bad database not set aside: %v", err)
	}
	if _, err := os.Stat(p + ".before-restore"); err == nil {
		t.Fatal("the current database was replaced")
	}
}

// A SQLite file that passes a quick check but clashes with the panel's schema is not usable.
func TestCheckDBSchemaClash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"CREATE TABLE agents(name TEXT)", "CREATE INDEX ad_sources ON agents(name)"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if err := CheckDB(p); err == nil {
		t.Fatal("a database the panel cannot open was accepted")
	}
}

// A table of the panel with columns missing (password_links without sam) is not usable either.
func TestCheckDBMissingColumns(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE password_links(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := CheckDB(p); err == nil || !strings.Contains(err.Error(), "password_links") {
		t.Fatalf("missing columns: %v", err)
	}
	good := filepath.Join(t.TempDir(), "good.db")
	st, err := Open(good)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := CheckDB(good); err != nil {
		t.Fatalf("a panel database was refused: %v", err)
	}
}
