package sambadc

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

//go:embed pwmigrate.py
var pwMigrateScript string

// PwSource is the source domain to read the password material from.
type PwSource struct {
	Host string `json:"host"`
	User string `json:"user"`
}

// PwUser is one user's outcome. Outcome is one of: would_set (plan only), set,
// ntlm_only, same, missing, builtin, rejected, error.
type PwUser struct {
	Sam     string `json:"sam"`
	Outcome string `json:"outcome"`
}

// PwResult is the result of a password-material plan or apply: the domain SIDs
// (to confirm source ≠ destination and the pin) and the per-user outcomes with
// their counts. It never carries any password material.
type PwResult struct {
	DestSID   string         `json:"dest_sid"`
	SourceSID string         `json:"source_sid"`
	Users     []PwUser       `json:"users"`
	Counts    map[string]int `json:"counts"`
}

// pwReqSource is the source block of the stdin request, including the password.
type pwReqSource struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// pwRequest is the JSON the helper reads on stdin. The source password travels only here (stdin), never a file on
// disk, an environment variable, or the command line.
type pwRequest struct {
	Op      string      `json:"op"`
	Source  pwReqSource `json:"source"`
	DestSID string      `json:"dest_sid"`
}

// MigratePasswords runs the embedded Python helper on this domain controller to
// plan (apply=false) or apply (apply=true) the password-material migration.
//
// The source replication account password is handed to the helper only on stdin
// (never a file, environment variable, or argument). The helper writes only
// outcomes on stdout; its stderr (which may hold Samba debug) is never returned,
// so no secret leaks through an error. destSID pins the destination domain and
// is required: the helper refuses when it is empty or does not match this DC.
//
// It never writes to the source: the helper uses a sealed GetNCChanges with the
// REPL_SECRET extended op and never calls UpdateRefs or sets DRS_ADD_REF.
func MigratePasswords(ctx context.Context, python string, apply bool, src PwSource, srcPassword, destSID string) (*PwResult, error) {
	if python == "" {
		python = "/usr/bin/python3"
	}
	op := "plan"
	if apply {
		op = "apply"
	}
	reqBody, err := json.Marshal(pwRequest{Op: op, DestSID: destSID,
		Source: pwReqSource{Host: src.Host, User: src.User, Password: srcPassword}})
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, python, "-I", "-s", "-B", "-c", pwMigrateScript)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(reqBody)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard // never surface helper stderr (may hold secret-adjacent debug)
	runErr := cmd.Run()
	var resp struct {
		OK     bool      `json:"ok"`
		Result *PwResult `json:"result"`
		Error  string    `json:"error"`
		Code   string    `json:"code"`
	}
	// the helper always prints a JSON object, even on its own error; a missing or unparseable one means it crashed
	if jsonErr := json.Unmarshal(stdout.Bytes(), &resp); jsonErr != nil {
		if runErr != nil {
			return nil, errors.New("password migration helper failed (see the agent's journal)")
		}
		return nil, fmt.Errorf("password migration: invalid helper output")
	}
	if !resp.OK {
		return nil, fmt.Errorf("password migration: %s", resp.Error)
	}
	if resp.Result == nil {
		return nil, errors.New("password migration: empty result")
	}
	return resp.Result, nil
}
