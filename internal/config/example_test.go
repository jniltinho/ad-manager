package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestExampleCoversKeys keeps config.toml.example a full reference: every key the binary reads (a toml tag of
// Config or of one of its sections) must be in it, in its section, set or commented out.
func TestExampleCoversKeys(t *testing.T) {
	b, err := os.ReadFile("../../config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	section := ""
	header := regexp.MustCompile(`^\[(\w+)\]`)
	key := regexp.MustCompile(`^#?\s*(\w+)\s*=`)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if m := header.FindStringSubmatch(line); m != nil {
			section = m[1]
		} else if m := key.FindStringSubmatch(line); m != nil {
			have[section+"."+m[1]] = true
		}
	}
	ct := reflect.TypeOf(Config{})
	for i := range ct.NumField() {
		f := ct.Field(i)
		tag := strings.Split(f.Tag.Get("toml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if f.Type.Kind() != reflect.Struct {
			if !have["."+tag] {
				t.Errorf("config.toml.example lacks the top-level key %s", tag)
			}
			continue
		}
		for j := range f.Type.NumField() {
			k := strings.Split(f.Type.Field(j).Tag.Get("toml"), ",")[0]
			if k != "" && k != "-" && !have[tag+"."+k] {
				t.Errorf("config.toml.example lacks [%s] %s", tag, k)
			}
		}
	}
}
