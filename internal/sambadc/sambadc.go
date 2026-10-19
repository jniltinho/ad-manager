// Package sambadc reads the health of a Samba AD domain controller, read only, for the agent installed on the DC:
// replication with the other DCs (`samba-tool drs showrepl --json`) and the consistency of the directory database
// (`samba-tool dbcheck --cross-ncs`, without --fix). `ntacl sysvolcheck` is left out: it opens passdb, which may
// write the domain SID into secrets.tdb.
package sambadc

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Runner runs a command without a shell and returns its combined output (err for a non-zero exit too).
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Link is one replication link of a naming context with another DC.
type Link struct {
	Direction   string `json:"direction"` // inbound (repsFrom: this DC pulls) or outbound (repsTo)
	NC          string `json:"nc"`
	DSA         string `json:"dsa"` // Site\Server of the other DC (its GUID when unknown)
	LastAttempt string `json:"last_attempt"`
	Message     string `json:"message"`
	Failures    int    `json:"failures"`
	LastSuccess string `json:"last_success"`
	Deleted     bool   `json:"deleted"`
	OK          bool   `json:"ok"`
}

// Replication is this DC's replication state.
type Replication struct {
	Links []Link `json:"links"`
	OK    bool   `json:"ok"` // every link healthy (a single DC has none)
}

// showrepl is the part of `samba-tool drs showrepl --json` read here.
type showrepl struct {
	RepsFrom []neighbour `json:"repsFrom"`
	RepsTo   []neighbour `json:"repsTo"`
}

type neighbour struct {
	NC          string `json:"NC dn"`
	GUID        string `json:"DSA objectGUID"`
	DSA         string `json:"DSA"`
	LastAttempt string `json:"last attempt time"`
	Message     string `json:"last attempt message"`
	Failures    int    `json:"consecutive failures"`
	LastSuccess string `json:"last success"`
	Deleted     bool   `json:"is deleted"`
}

// never is how samba-tool prints a zero NTTIME.
const never = "NTTIME(0)"

// ParseReplication turns `samba-tool drs showrepl --json` into the links of this DC. A link is healthy when it has
// no consecutive failure, has succeeded at least once and points to a DC that still exists.
func ParseReplication(out []byte) (*Replication, error) {
	var raw showrepl
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("drs showrepl: invalid JSON: %w", err)
	}
	r := &Replication{Links: []Link{}, OK: true}
	for _, dir := range []struct {
		name string
		list []neighbour
	}{{"inbound", raw.RepsFrom}, {"outbound", raw.RepsTo}} {
		for _, n := range dir.list {
			dsa := n.DSA
			if dsa == "" {
				dsa = n.GUID
			}
			l := Link{Direction: dir.name, NC: n.NC, DSA: dsa, LastAttempt: n.LastAttempt, Message: n.Message,
				Failures: n.Failures, LastSuccess: n.LastSuccess, Deleted: n.Deleted}
			l.OK = n.Failures == 0 && n.LastSuccess != "" && n.LastSuccess != never && !n.Deleted
			r.OK = r.OK && l.OK
			r.Links = append(r.Links, l)
		}
	}
	return r, nil
}

// ReadReplication runs showrepl on this DC.
func ReadReplication(ctx context.Context, run Runner) (*Replication, error) {
	out, err := run(ctx, "samba-tool", "drs", "showrepl", "--json")
	if err != nil {
		return nil, err
	}
	return ParseReplication([]byte(out))
}

// Check is the result of one consistency check.
type Check struct {
	Name    string `json:"name"` // dbcheck
	OK      bool   `json:"ok"`
	Objects int    `json:"objects"` // objects checked
	Errors  int    `json:"errors"`  // errors found
	Output  string `json:"output"`  // the last lines of the command's output
}

// Checks is the result of the consistency checks and when they ran.
type Checks struct {
	At     time.Time `json:"at"`
	Checks []Check   `json:"checks"`
	OK     bool      `json:"ok"`
}

var dbcheckSummary = regexp.MustCompile(`Checked (\d+) objects \((\d+) errors\)`)

// tail keeps the last n lines of out.
func tail(out string, n int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// RunChecks runs dbcheck on all partitions, read only (no --fix).
func RunChecks(ctx context.Context, run Runner) *Checks {
	c := &Checks{At: time.Now().UTC(), OK: true}
	out, err := run(ctx, "samba-tool", "dbcheck", "--cross-ncs")
	db := Check{Name: "dbcheck", OK: err == nil, Output: tail(out, 20)}
	if m := dbcheckSummary.FindStringSubmatch(out); m != nil {
		db.Objects, _ = strconv.Atoi(m[1])
		db.Errors, _ = strconv.Atoi(m[2])
		db.OK = db.OK && db.Errors == 0
	} else {
		db.OK = false
	}
	c.Checks = []Check{db}
	c.OK = db.OK
	return c
}
