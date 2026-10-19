package audit

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLogHasNoPassword(t *testing.T) {
	var buf bytes.Buffer
	SetWriter(&buf)
	Log("admin", "10.0.0.1", "password.reset", "joao.silva", nil)
	Log("admin", "10.0.0.1", "password.reset", "maria", errors.New("a senha não atende à política do domínio"))
	out := buf.String()
	for _, want := range []string{`"actor":"admin"`, `"action":"password.reset"`, `"target":"joao.silva"`, `"result":"ok"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("expected 2 lines, got %q", out)
	}
}
