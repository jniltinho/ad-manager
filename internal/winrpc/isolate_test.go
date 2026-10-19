package winrpc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// TestMain doubles as the helper process: `<test binary> winrpc` answers like `ad-manager winrpc`, with the
// failures the panel must survive chosen by the user name.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "winrpc" {
		var req Request
		_ = json.NewDecoder(os.Stdin).Decode(&req)
		switch req.Cred.User {
		case "panic":
			panic("malformed SMB reply")
		case "hang":
			time.Sleep(time.Hour)
		case "flood":
			big := make([]byte, 1<<20)
			for {
				if _, err := os.Stdout.Write(big); err != nil {
					os.Exit(1)
				}
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(Response{Zones: []Zone{{Name: req.Target.Host}}})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestIsolated(t *testing.T) {
	iso := Isolated{Exe: os.Args[0], Timeout: 2 * time.Second}
	z, err := iso.DNSZones(context.Background(), Target{Addr: "10.0.0.1", Host: "dc1.empresa.net"}, Cred{User: "ana"})
	if err != nil || len(z) != 1 || z[0].Name != "dc1.empresa.net" {
		t.Fatalf("answer: %+v %v", z, err)
	}
	if _, err := iso.DNSZones(context.Background(), Target{Host: "x"}, Cred{User: "panic"}); err == nil {
		t.Fatal("a crashed helper must be an error (and the caller still runs)")
	}
	if _, err := iso.DNSZones(context.Background(), Target{Host: "x"}, Cred{User: "flood"}); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("a flooding helper: %v", err)
	}
	start := time.Now()
	if _, err := iso.Sessions(context.Background(), Target{Host: "x"}, Cred{User: "hang"}); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("a hung helper: %v after %v", err, time.Since(start))
	}
}
