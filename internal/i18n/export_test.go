package i18n

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestExportForWeb writes the catalog the web tests use (frontend/src/testdata),
// so both sides test the same data. Run with UPDATE_I18N=1 after changing it.
func TestExportForWeb(t *testing.T) {
	b, _ := json.MarshalIndent(PTCatalog(), "", " ")
	const path = "../../frontend/src/testdata/i18n-pt.json"
	if os.Getenv("UPDATE_I18N") != "" {
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cur, err := os.ReadFile(path)
	var got Catalog
	if err != nil || json.Unmarshal(cur, &got) != nil || !reflect.DeepEqual(got, PTCatalog()) {
		t.Fatalf("%s is out of date: UPDATE_I18N=1 go test ./internal/i18n", path)
	}
}
