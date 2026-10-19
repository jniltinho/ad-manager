package server

import (
	"slices"
	"testing"

	"ad-manager/internal/shares"
)

func TestPlanShares(t *testing.T) {
	t.Parallel()
	current := []shares.Share{
		{Name: "Financeiro", Path: "/srv/shares/Financeiro", RW: []string{`EMP\Financeiro`, `EMP\Domain Admins`}, Recycle: true, Audit: true},
		{Name: "Publico", Path: "/srv/shares/Publico", RW: []string{`EMP\Domain Admins`}},
	}
	rows := []map[string]string{
		{"share": "NFe$", "path": "/srv/shares/Financeiro/NFe", "write_groups": `EMP\Financeiro`},
		{"share": "financeiro", "path": "/srv/shares/Financeiro", "write_groups": `EMP\Domain Admins|EMP\Financeiro`},
		{"share": "Publico", "path": "/srv/shares/Publico", "write_groups": `EMP\Domain Admins`, "read_groups": `EMP\Domain Users`},
	}
	var got []string
	plan, reqs := planShares(rows, current)
	for _, a := range plan {
		got = append(got, a.Kind+":"+a.Target)
	}
	want := []string{"skip:financeiro", "share.update:Publico", "share.create:NFe$"}
	if !slices.Equal(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}
	if r := shares.FromCSV(rows[0]); !r.Hidden || !r.CreatePath || len(r.RW) != 1 {
		t.Errorf("FromCSV: %+v", r)
	}
	// an older file without the option columns keeps what the share has
	if r := reqs["financeiro"]; !r.Recycle || !r.Audit || r.ABE {
		t.Errorf("options of an older file: %+v", r)
	}
	// a file that turns an option off: an update
	rows[1]["recycle_bin"] = "no"
	plan, reqs = planShares(rows, current)
	if plan[0].Kind != "share.update" || reqs["financeiro"].Recycle || !reqs["financeiro"].Audit {
		t.Errorf("recycle bin off: %+v %+v", plan[0], reqs["financeiro"])
	}
}
