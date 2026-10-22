package sambadc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubPython writes an executable that ignores its -c script and prints body on stdout and noise on stderr, so the
// test exercises MigratePasswords without python3-samba. It reads the request on stdin to prove it is passed there.
func stubPython(t *testing.T, stdout, stderr string, exit int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "py")
	script := "#!/bin/sh\ncat >/dev/null\n"
	if stderr != "" {
		script += "printf '%s' " + shellQuote(stderr) + " >&2\n"
	}
	script += "printf '%s' " + shellQuote(stdout) + "\nexit " + itoa(exit) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func TestMigratePasswordsParsesOutcomes(t *testing.T) {
	py := stubPython(t, `{"ok":true,"result":{"dest_sid":"S-1-5-21-2","source_sid":"S-1-5-21-1","users":[{"sam":"ana","outcome":"set"},{"sam":"guest","outcome":"builtin"}],"counts":{"set":1,"builtin":1}}}`, "", 0)
	res, err := MigratePasswords(context.Background(), py, true, PwSource{Host: "h", User: "u"}, "secret", "S-1-5-21-2")
	if err != nil {
		t.Fatal(err)
	}
	if res.DestSID == res.SourceSID {
		t.Error("source and destination SID must differ in this fixture")
	}
	if len(res.Users) != 2 || res.Users[0].Outcome != "set" || res.Counts["builtin"] != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

// the helper's own error object is surfaced (it is secret-free by construction); its stderr never is.
func TestMigratePasswordsHelperError(t *testing.T) {
	py := stubPython(t, `{"ok":false,"error":"source and destination are the same domain","code":"same-domain"}`,
		"SECRET-NT-HASH-0123456789abcdef should never surface", 0)
	_, err := MigratePasswords(context.Background(), py, false, PwSource{Host: "h", User: "u"}, "secret", "S-1-5-21-2")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "same domain") {
		t.Errorf("want the helper's error, got %q", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("stderr must never surface in the error: %q", err)
	}
}

// a crashed helper (no JSON on stdout) never leaks its stderr either.
func TestMigratePasswordsCrash(t *testing.T) {
	py := stubPython(t, "", "Traceback: SECRET leaked", 1)
	_, err := MigratePasswords(context.Background(), py, false, PwSource{Host: "h", User: "u"}, "secret", "S-1-5-21-2")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("stderr must never surface: %q", err)
	}
}
