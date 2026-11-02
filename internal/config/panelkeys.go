package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// PanelKeys are the config.toml keys the panel may change through its local agent (the panel:config task): the [ad]
// keys, read only by the panel, and two write-mode switches of the agent itself, turned on from the panel by the
// operator's choice (they give the panel's administrators the agent's GPO import, password migration and DC file
// modes). Other keys the root agent reads (redis, samba, the logs, the rest of [agent]) stay out.
var PanelKeys = []string{"ad.url", "ad.domain", "ad.base_dn", "ad.start_tls", "ad.ca_file", "ad.insecure_skip_verify", "ad.admin_group",
	"agent.dc_file_server", "agent.import_modes"}

// PanelKeyEnv maps a panel key to the environment variable that overrides it (Load).
var PanelKeyEnv = map[string]string{"ad.url": "AD_URL", "ad.domain": "AD_DOMAIN", "ad.base_dn": "AD_BASE_DN",
	"ad.ca_file": "AD_CA_FILE", "ad.admin_group": "AD_ADMIN_GROUP", "ad.insecure_skip_verify": "AD_INSECURE_SKIP_VERIFY"}

// PanelField returns a pointer to the field of a panel key (*string or *bool), nil for any other key.
func (c *Config) PanelField(key string) any {
	switch key {
	case "ad.url":
		return &c.AD.URL
	case "ad.domain":
		return &c.AD.Domain
	case "ad.base_dn":
		return &c.AD.BaseDN
	case "ad.start_tls":
		return &c.AD.StartTLS
	case "ad.ca_file":
		return &c.AD.CAFile
	case "ad.insecure_skip_verify":
		return &c.AD.InsecureSkipVerify
	case "ad.admin_group":
		return &c.AD.AdminGroup
	case "agent.dc_file_server":
		return &c.Agent.DCFileServer
	case "agent.import_modes":
		return &c.Agent.ImportModes
	}
	return nil
}

var reDomain = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)

// SetPanelKeys checks keys (allowlist, types, formats) and sets them on c; the message names the key.
func (c *Config) SetPanelKeys(keys map[string]any) error {
	if len(keys) == 0 {
		return errors.New("nothing to change")
	}
	for k, v := range keys {
		switch p := c.PanelField(k).(type) {
		case *string:
			s, ok := v.(string)
			if !ok || strings.ContainsAny(s, "\r\n\x00") || len(s) > 512 {
				return fmt.Errorf("%s: invalid value", k)
			}
			*p = strings.TrimSpace(s)
			keys[k] = *p // the caller writes the normalized value
		case *bool:
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("%s: true or false", k)
			}
			*p = b
		default:
			return fmt.Errorf("%s: not editable from the panel", k)
		}
	}
	a := c.AD
	if u, err := url.Parse(a.URL); err != nil || (u.Scheme != "ldaps" && u.Scheme != "ldap") || u.Host == "" || u.Path != "" {
		return errors.New("ad.url: enter ldaps://host or ldap://host[:port]")
	}
	if !reDomain.MatchString(a.Domain) {
		return errors.New("ad.domain: enter the DNS name of the domain (example.local)")
	}
	if a.BaseDN != "" && !strings.HasPrefix(strings.ToUpper(a.BaseDN), "DC=") {
		return errors.New("ad.base_dn: empty or DC=…,DC=…")
	}
	if _, ok := keys["ad.ca_file"]; ok && a.CAFile != "" {
		fi, err := os.Stat(a.CAFile)
		if !filepath.IsAbs(a.CAFile) || err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
			return errors.New("ad.ca_file: the absolute path of a PEM file (up to 1 MiB)")
		}
	}
	return nil
}

// SetKeys returns text with the given "table.key" values set, keeping comments, order and every other line; a missing
// key is added under its table, a missing table at the end. A changed line loses its inline comment.
func SetKeys(text string, keys map[string]any) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	done := map[string]bool{}
	lastOf := map[string]int{} // table → index of its last line (header or key)
	table := ""
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[") {
			if end := strings.Index(t, "]"); end > 0 {
				table = strings.TrimSpace(t[1:end])
				lastOf[table] = i
			}
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if !ok || strings.HasPrefix(t, "#") {
			continue
		}
		lastOf[table] = i
		full := strings.TrimSpace(k)
		if table != "" {
			full = table + "." + full
		}
		if v, ok := keys[full]; ok {
			enc, err := tomlValue(v)
			if err != nil {
				return "", err
			}
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			lines[i] = indent + strings.TrimSpace(k) + " = " + enc + "\n"
			done[full] = true
		}
	}
	var missing []string
	for k := range keys {
		if !done[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	var tail strings.Builder
	added := map[string]bool{}
	for _, k := range missing {
		tbl, key, _ := strings.Cut(k, ".")
		enc, err := tomlValue(keys[k])
		if err != nil {
			return "", err
		}
		line := key + " = " + enc + "\n"
		if i, ok := lastOf[tbl]; ok {
			if !strings.HasSuffix(lines[i], "\n") {
				lines[i] += "\n"
			}
			lines[i] += line
			continue
		}
		if !added[tbl] {
			tail.WriteString("\n[" + tbl + "]\n")
			added[tbl] = true
		}
		tail.WriteString(line)
	}
	out := strings.Join(lines, "")
	if tail.Len() > 0 {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += tail.String()
	}
	return out, nil
}

// Edit validates keys against the values of old (the keys not sent keep them) and returns old with them set,
// checked by CheckEdit.
func Edit(old []byte, keys map[string]any) ([]byte, error) {
	cur := defaults() // a key the file leaves out has Load's default
	if err := toml.Unmarshal(old, cur); err != nil {
		return nil, fmt.Errorf("config.toml: %w", err)
	}
	if err := cur.SetPanelKeys(keys); err != nil { // normalizes keys
		return nil, err
	}
	text, err := SetKeys(string(old), keys)
	if err != nil {
		return nil, err
	}
	if err := CheckEdit(old, []byte(text), keys); err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// tomlValue encodes one string or bool as TOML.
func tomlValue(v any) (string, error) {
	b, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return "", err
	}
	_, enc, _ := strings.Cut(strings.TrimSpace(string(b)), "=")
	return strings.TrimSpace(enc), nil
}

// CheckEdit makes sure edited is old with exactly keys changed: the whole TOML documents are compared, unknown keys
// and multiline values included, and edited still decodes as a configuration.
func CheckEdit(old, edited []byte, keys map[string]any) error {
	var want, got map[string]any
	if err := toml.Unmarshal(old, &want); err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	if err := toml.Unmarshal(edited, &got); err != nil {
		return fmt.Errorf("edited config.toml: %w", err)
	}
	if err := toml.Unmarshal(edited, defaults()); err != nil {
		return fmt.Errorf("edited config.toml: %w", err)
	}
	if want == nil {
		want = map[string]any{}
	}
	for k, v := range keys {
		tbl, key, _ := strings.Cut(k, ".")
		t, ok := want[tbl].(map[string]any)
		if !ok {
			t = map[string]any{}
			want[tbl] = t
		}
		t[key] = v
	}
	if !reflect.DeepEqual(want, got) {
		return errors.New("the edited config.toml does not match the change (unusual layout: edit it by hand)")
	}
	return nil
}

// RestartsAgent tells whether a change of key takes effect only when the agent restarts ([agent]); the others need
// the panel restarted.
func RestartsAgent(key string) bool { return strings.HasPrefix(key, "agent.") }
