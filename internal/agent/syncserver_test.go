package agent

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ad-manager/internal/backup"
	"ad-manager/internal/config"
	"ad-manager/internal/syncer"
)

// Every share of the source goes to the folder of the share of the same name here; the administrative ones, sysvol
// and netlogon never; the others skipped with the reason.
func TestMapTargets(t *testing.T) {
	paths := map[string]string{
		"financeiro": "/srv/shares/Financeiro", "publico": "/srv/shares/Users/Publico", "comercial": "/srv/shares/Comercial",
		"contrato": "/srv/shares/Comercial/Contrato", "alias": "/srv/shares/Financeiro/", "faturamento sefaz$": "/srv/shares/Faturamento Sefaz",
		"outside": "/etc", "sysvol": "/var/lib/samba/sysvol",
	}
	valid := func(p string) error {
		if !strings.HasPrefix(p, "/srv/shares/") {
			return errors.New("must be a folder inside a share root")
		}
		return nil
	}
	got := mapTargets([]string{"Publico", "C$", "ADMIN$", "IPC$", "print$", "SYSVOL", "NETLOGON", "Financeiro", "Contrato",
		"Comercial", "Projetos", "Alias", "Faturamento Sefaz$", "Outside", "Printers"}, paths, []string{"Printers"}, valid)
	want := []syncTarget{
		{Share: "Alias", Dest: "/srv/shares/Financeiro"},
		{Share: "Comercial", Dest: "/srv/shares/Comercial"},
		{Share: "Contrato", Dest: "/srv/shares/Comercial/Contrato", Skip: "inside Comercial: copied with it"},
		{Share: "Faturamento Sefaz$", Dest: "/srv/shares/Faturamento Sefaz"},
		{Share: "Financeiro", Dest: "/srv/shares/Financeiro", Skip: "same folder as Alias"},
		{Share: "Outside", Dest: "/etc", Skip: "must be a folder inside a share root"},
		{Share: "Printers", Skip: "reserved on this server"},
		{Share: "Projetos", Skip: "not published on this server"},
		{Share: "Publico", Dest: "/srv/shares/Users/Publico"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// A server job names the server alone and only copies.
func TestValidateServerJob(t *testing.T) {
	a := &Agent{files: true}
	ok := SyncJob{ID: "fs-all", Kind: "server", Source: syncer.Source{Host: "192.0.2.10", Auth: "ntlm", User: "administrator"}}
	if err := a.validateSync(&ok); err != nil || ok.Mode != syncer.ModeCopy || ok.Dest != "" {
		t.Fatalf("valid job: %v %+v", err, ok)
	}
	for name, mod := range map[string]func(*SyncJob){
		"share":  func(j *SyncJob) { j.Source.Share = "Financeiro" },
		"path":   func(j *SyncJob) { j.Source.Path = "x" },
		"mirror": func(j *SyncJob) { j.Mode = syncer.ModeMirror },
		"kind":   func(j *SyncJob) { j.Kind = "everything" },
		"host":   func(j *SyncJob) { j.Source.Host = "bad host" },
	} {
		j := ok
		j.Mode = ""
		mod(&j)
		if err := a.validateSync(&j); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A server run's result sums its shares', their sample names under the share's name.
func TestAddResult(t *testing.T) {
	sum := &syncer.Result{Plan: syncer.Plan{Sample: map[string][]string{}}, Checked: true}
	addResult(sum, &syncer.Result{Plan: syncer.Plan{New: 2, NewBytes: 10, Sample: map[string][]string{"new": {"a.txt"}}}, Transfers: 2, Bytes: 10, Checked: true}, "Fin")
	addResult(sum, &syncer.Result{Plan: syncer.Plan{New: 1, Same: 4}, Transfers: 1, Bytes: 5, Checked: true}, "Pub")
	if sum.Plan.New != 3 || sum.Plan.Same != 4 || sum.Transfers != 3 || sum.Bytes != 15 || !sum.Checked ||
		!reflect.DeepEqual(sum.Plan.Sample["new"], []string{"Fin/a.txt"}) {
		t.Fatalf("sum = %+v", sum)
	}
	addResult(sum, nil, "Gone")
	if sum.Checked {
		t.Fatal("a share without a result left the run checked")
	}
}

// A server job's share keeps its quarantine under its own name (<job>-<run>-s<n>): written, read back, listed.
func TestServerPartQuarantine(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	a := &Agent{cfg: config.Agent{ShareRoots: []string{root}}, backups: &backup.Store{Dir: state}}
	q := Quarantine{Name: "fs-all-20261024T190917Z-ad05936b-s2", Job: "fs-all", Run: "20261024T190917Z-ad05936b", Part: "-s2",
		Dest: root + "/Financeiro", Path: filepath.Join(root, SyncDir, "fs-all-20261024T190917Z-ad05936b-s2")}
	if err := a.writeQuarantine(q); err != nil {
		t.Fatal(err)
	}
	got, err := a.quarantine(q.Name)
	if err != nil || got.Part != "-s2" || got.Run != q.Run {
		t.Fatalf("read back: %+v %v", got, err)
	}
	for name, bad := range map[string]Quarantine{
		"part not in the name": {Name: q.Name, Job: q.Job, Run: q.Run, Path: q.Path},
		"odd part":             {Name: "fs-all-20261024T190917Z-ad05936b-x2", Job: q.Job, Run: q.Run, Part: "-x2", Path: q.Path},
	} {
		if a.quarantineOK(bad) {
			t.Errorf("%s: accepted", name)
		}
	}
}
