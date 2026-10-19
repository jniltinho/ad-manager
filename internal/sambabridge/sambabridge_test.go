package sambabridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func fake(resp string, seen *map[string]any) func(context.Context, []byte, []*os.File) ([]byte, error) {
	return func(_ context.Context, in []byte, _ []*os.File) ([]byte, error) {
		if seen != nil {
			_ = json.Unmarshal(in, seen)
		}
		return []byte(resp), nil
	}
}

func TestCall(t *testing.T) {
	var seen map[string]any
	b := &Bridge{run: fake(`{"ok":true,"result":{"owner":"S-1-22-1-0","aces":[{"sid":"S-1-5-18","access":"full"}]}}`, &seen)}
	acl, err := b.SetFolderACL(context.Background(), nil, FolderACL{Path: "/srv/x", Owner: "S-1-22-1-0", Entries: []Entry{{SID: "S-1-5-18", Access: "full"}}})
	if err != nil || acl.Owner != "S-1-22-1-0" || len(acl.ACEs) != 1 || acl.ACEs[0].Access != "full" {
		t.Fatalf("got %+v, %v", acl, err)
	}
	if seen["op"] != "set_folder_acl" || seen["args"].(map[string]any)["owner_sid"] != "S-1-22-1-0" {
		t.Fatalf("request: %v", seen)
	}

	b.run = fake(`{"ok":false,"error":"path must be absolute","code":"invalid"}`, nil)
	if _, err := b.GetACL(context.Background(), nil, "x", nil, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid input: got %v, want ErrInvalid", err)
	}
	b.run = fake(`{"ok":false,"error":"NTSTATUSError: access denied","code":"samba"}`, nil)
	if _, err := b.GetACL(context.Background(), nil, "/srv/x", nil, ""); err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("samba error: got %v", err)
	}
	b.run = fake(`not json`, nil)
	if c := b.Capabilities(context.Background()); c.Usable() || c.Error == "" {
		t.Fatalf("broken helper must not be usable: %+v", c)
	}
}

// TestCappedCopy: the helper's output is copied from a pipe with io.Copy, which must go through Write (the cap).
func TestCappedCopy(t *testing.T) {
	c := &capped{max: 10}
	if _, err := io.Copy(c, strings.NewReader(strings.Repeat("x", 100))); err != nil || len(c.Bytes()) != 10 {
		t.Fatalf("kept %d bytes, %v", len(c.Bytes()), err)
	}
}

// TestGetACLs: one helper process for several folders, passed as descriptors 3, 4, … in order; a folder the helper
// could not read has no ACL and its own error, the others still come.
func TestGetACLs(t *testing.T) {
	var seen map[string]any
	var files int
	b := &Bridge{GoRead: true, run: func(_ context.Context, in []byte, fs []*os.File) ([]byte, error) {
		files = len(fs)
		_ = json.Unmarshal(in, &seen)
		return []byte(`{"ok":true,"result":{"items":[{"acl":{"owner":"S-1-5-18","aces":[]}},{"error":"/srv/b changed","code":"invalid"}]}}`), nil
	}}
	a, _ := os.Open(os.TempDir())
	defer a.Close()
	acls, errs, err := b.GetACLs(context.Background(), []*os.File{a, a}, []FolderRef{{Path: "/srv/a"}, {Path: "/srv/b"}}, []string{"/srv"})
	if err != nil || files != 2 || seen["op"] != "get_acls" {
		t.Fatalf("err %v, %d descriptors, request %v", err, files, seen)
	}
	if acls[0] == nil || acls[0].Owner != "S-1-5-18" || errs[0] != nil || acls[1] != nil || errs[1] == nil {
		t.Fatalf("got %+v %v", acls, errs)
	}
	if _, _, err := b.GetACLs(context.Background(), []*os.File{a}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a descriptor without its folder: %v", err)
	}
}
