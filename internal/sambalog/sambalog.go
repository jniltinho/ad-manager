// Package sambalog lists and reads Samba's log files (log.samba, log.smbd,
// log.winbindd, log.<client>…) for the Logs page. It only reads regular files
// named log.* directly inside the configured directory, never follows symlinks
// and only reads the end of each file.
package sambalog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// tailBytes is how much of the end of a file is read.
// ponytail: fixed window; raise it or add paging if older entries are needed.
const tailBytes = 2 << 20

// ErrInvalidName is returned for a name that is not a log file in the directory.
var ErrInvalidName = errors.New("invalid log file")

var reName = regexp.MustCompile(`^log\.[A-Za-z0-9_.$@-]*$`)

// File is a log file in the directory.
type File struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Entry is one log message: Samba writes a header line
// "[2026/10/17 19:05:07.925294,  0] source.c:85(function)" followed by
// indented message lines.
type Entry struct {
	Time    string `json:"time"`    // as written by Samba (local time of the server)
	Level   int    `json:"level"`   // debug level: 0 = error/important
	Source  string `json:"source"`  // file:line(function)
	Message string `json:"message"` // message lines joined with \n
}

// List returns the log files in dir, most recently modified first. Empty files
// are kept (they show which daemons exist) after the others.
func List(dir string) ([]File, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range ents {
		if !e.Type().IsRegular() || !reName.MatchString(e.Name()) {
			continue // skips directories (cores/) and symlinks
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, File{Name: e.Name(), Size: fi.Size(), Modified: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Size == 0) != (out[j].Size == 0) {
			return out[i].Size != 0
		}
		return out[i].Modified.After(out[j].Modified)
	})
	return out, nil
}

var reHeader = regexp.MustCompile(`^\[(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})(?:\.\d+)?,\s*(\d+)[^\]]*\]\s*(.*)$`)

// Read returns up to n entries from the end of dir/name, newest first, keeping
// only those that contain q (case-insensitive) when q is not empty.
func Read(dir, name string, n int, q string) ([]Entry, error) {
	if !reName.MatchString(name) || strings.ContainsAny(name, `/\`) {
		return nil, ErrInvalidName
	}
	path := filepath.Join(dir, name)
	li, err := os.Lstat(path)
	if err != nil || !li.Mode().IsRegular() { // also refuses symlinks
		return nil, fmt.Errorf("%w: %s", ErrInvalidName, name)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(li, fi) { // swapped for something else between the two calls
		return nil, fmt.Errorf("%w: %s", ErrInvalidName, name)
	}
	start := max(fi.Size()-tailBytes, 0)
	b, err := io.ReadAll(io.NewSectionReader(f, start, fi.Size()-start))
	if err != nil {
		return nil, err
	}
	if start > 0 { // the first line was cut in half
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return newest(parse(b), n, q), nil
}

// parse splits the text into entries; lines before the first header (or in a
// file without headers) become entries of their own.
func parse(b []byte) []Entry {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if m := reHeader.FindStringSubmatch(line); m != nil {
			lvl, _ := strconv.Atoi(m[2])
			out = append(out, Entry{Time: m[1], Level: lvl, Source: m[3]})
			continue
		}
		msg := strings.TrimSpace(line)
		if msg == "" {
			continue
		}
		if len(out) == 0 || (line == msg && out[len(out)-1].Message != "") {
			out = append(out, Entry{Level: -1, Message: msg}) // free text, no header
			continue
		}
		e := &out[len(out)-1]
		if e.Message != "" {
			e.Message += "\n"
		}
		e.Message += msg
	}
	return out
}

func newest(all []Entry, n int, q string) []Entry {
	q = strings.ToLower(q)
	out := []Entry{}
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		e := all[i]
		if q != "" && !strings.Contains(strings.ToLower(e.Message+" "+e.Source), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}
