package winshares

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

const dom = "S-1-5-21-1-2-3"

// outputs is real (shortened) rpcclient/smbcacls output against Windows 2008.
var outputs = map[string]string{
	"netshareenumall": "netname: ADMIN$\n\tremark:\tRemote Admin\n\tpath:\tC:\\Windows\n" +
		"netname: Financeiro\n\tremark:\t\n\tpath:\tE:\\Financeiro\n" +
		"netname: Notas Fiscais$\n\tremark:\tNotas\n\tpath:\tE:\\Financeiro\\NFe\n" +
		"netname: Publico\n\tremark:\t\n\tpath:\tE:\\Publico\n",
	`netsharegetinfo "Financeiro" 502`:     sd("500:0x1f01ff", "512:0x1f01ff", "1103:0x1301bf"),
	`netsharegetinfo "Notas Fiscais$" 502`: sd("512:0x1f01ff", "1162:0x1200a9"),
	`netsharegetinfo "Publico" 502`:        sd("512:0x1f01ff", "S-1-1-0:0x1f01ff"),
	"//h/Financeiro": "OWNER:EMPRESA\\Administrator\nACL:EMPRESA\\Diretoria:ALLOWED/OI|CI/FULL\n" +
		"ACL:EMPRESA\\Financeiro:ALLOWED/OI|CI/FULL\nACL:BUILTIN\\Administrators:ALLOWED/OI|CI|I/FULL\n" +
		"ACL:CREATOR OWNER:ALLOWED/OI|CI|IO|I/0x10000000\n",
	"//h/Notas Fiscais$": "ACL:EMPRESA\\Financeiro:ALLOWED/OI|CI|I/FULL\nACL:EMPRESA\\0000045E:ALLOWED/OI|CI/FULL\n",
	"//h/Publico":        "ACL:BUILTIN\\Users:ALLOWED/OI|CI|I/READ\nACL:EMPRESA\\Comercial:ALLOWED/OI|CI/RWXD\nACL:EMPRESA\\Estagio:DENIED/OI|CI/FULL\n",
}

// sd builds a fake netsharegetinfo security descriptor from "rid:mask" pairs.
func sd(aces ...string) string {
	var b strings.Builder
	b.WriteString("DACL\n\tACL\tNum ACEs:\t3\n")
	for _, a := range aces {
		sid, mask, _ := strings.Cut(a, ":")
		if !strings.HasPrefix(sid, "S-") {
			sid = dom + "-" + sid
		}
		b.WriteString("\tACE\n\t\ttype: ACCESS ALLOWED (0) flags: 0x00 \n\t\tPermissions: " + mask + ": X\n\t\tSID: " + sid + "\n\n")
	}
	return b.String()
}

// fake is a Runner that replays outputs.
func fake(_ context.Context, name string, args ...string) (string, error) {
	if name == "smbcacls" {
		return outputs[args[2]], nil
	}
	c := args[len(args)-1]
	if strings.HasPrefix(c, "lookupsids") {
		names := map[string]string{"500": `EMPRESA\Administrator`, "512": `EMPRESA\Domain Admins`, "1103": `EMPRESA\Financeiro`, "1162": `EMPRESA\ana.lima`}
		var b strings.Builder
		for _, sid := range strings.Fields(c)[1:] {
			n := names[sid[strings.LastIndex(sid, "-")+1:]]
			if sid == "S-1-1-0" {
				n = `\Everyone`
			}
			b.WriteString(sid + " " + n + " (1)\n")
		}
		return b.String(), nil
	}
	return outputs[c], nil
}

func TestCollectAndPlan(t *testing.T) {
	t.Parallel()
	shares, err := Collect(context.Background(), fake, "h", "/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 3 {
		t.Fatalf("expected 3 data shares (without ADMIN$), got %d", len(shares))
	}
	d := Directory{
		NetBIOS: "EMPRESA",
		Groups:  map[string][]string{"Financeiro": {"ana.lima", "bia"}, "Diretoria": {"carlos"}},
		Users:   map[string]bool{"ana.lima": true, "bia": true, "carlos": true},
	}
	plans := Plan(shares, d, map[string]string{`E:\`: "/srv/shares"})
	got := map[string]SharePlan{}
	for _, p := range plans {
		got[p.Name] = p
	}
	tests := []struct {
		name, share, path string
		rw, ro, rou, full []string
	}{
		// the share grants Change to Financeiro; Diretoria only has NTFS → left out
		{"share and NTFS intersection", "Financeiro", "/srv/shares/Financeiro", []string{`EMPRESA\Financeiro`}, nil, nil, []string{`EMPRESA\Domain Admins`}},
		// direct user with Read on the share and write inherited through a group on NTFS → read
		{"user and hidden name", "Notas Fiscais$", "/srv/shares/Financeiro/NFe", nil, nil, []string{`EMPRESA\ana.lima`}, []string{`EMPRESA\Domain Admins`}},
		// Everyone on the share: NTFS decides; Users becomes Domain Users
		{"everyone on the share", "Publico", "/srv/shares/Publico", []string{`EMPRESA\Comercial`}, []string{`EMPRESA\Domain Users`}, nil, []string{`EMPRESA\Domain Admins`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := got[tt.share]
			if p.Path != tt.path || !slices.Equal(p.RW, tt.rw) || !slices.Equal(p.RO, tt.ro) || !slices.Equal(p.ROUsers, tt.rou) || !slices.Equal(p.Full, tt.full) {
				t.Errorf("%s: path=%s rw=%v ro=%v rou=%v full=%v", tt.share, p.Path, p.RW, p.RO, p.ROUsers, p.Full)
			}
		})
	}
	if !slices.ContainsFunc(got["Publico"].Warnings, func(a string) bool { return strings.Contains(a, "Estagio") }) {
		t.Errorf("deny ACE not warned: %v", got["Publico"].Warnings)
	}
	if !slices.ContainsFunc(got["Notas Fiscais$"].Warnings, func(a string) bool { return strings.Contains(a, "0000045E") }) {
		t.Errorf("deleted account not warned: %v", got["Notas Fiscais$"].Warnings)
	}
	if plans[len(plans)-1].Name != "Notas Fiscais$" {
		t.Errorf("subfolder must come after the parent folder: %v", plans[len(plans)-1].Name)
	}
}

func TestMaskLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mask uint32
		want int
	}{
		{"full control", 0x001F01FF, levelFull},
		{"generic all", 0x10000000, levelFull},
		{"modify (change)", 0x001301BF, levelWrite},
		{"partial: write + WRITE_DAC + WRITE_OWNER", 0x000C0002, levelWrite},
		{"read and execute", 0x001200A9, levelRead},
		{"nothing", 0x00100000, levelNone},
	}
	for _, tt := range tests {
		if got := maskLevel(tt.mask); got != tt.want {
			t.Errorf("%s: maskLevel(%#x) = %d, want %d", tt.name, tt.mask, got, tt.want)
		}
	}
	if rightsLevel("FULL") != levelFull || rightsLevel("CHANGE") != levelWrite || rightsLevel("READ") != levelRead {
		t.Error("smbcacls FULL/CHANGE/READ must map to full/write/read")
	}
}

func TestValidHostAndAuthFile(t *testing.T) {
	for _, h := range []string{"fs01", "fs01.empresa.net", "192.168.10.91"} {
		if ValidHost(h) != nil {
			t.Errorf("%q refused", h)
		}
	}
	for _, h := range []string{"-c", "a/b", "", "a b", `\\fs01`, "a;b"} {
		if ValidHost(h) == nil {
			t.Errorf("%q accepted", h)
		}
	}
	dir := t.TempDir()
	p, err := AuthFile(dir, "admin@empresa.net", "s3cr=t", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "username=admin\npassword=s3cr=t\ndomain=EMPRESA\n" || fi.Mode().Perm() != 0o600 {
		t.Fatalf("auth file %q %v", b, fi.Mode())
	}
	if _, err := AuthFile(dir, "admin", "a\nusername=x", "D"); err == nil {
		t.Fatal("a password with a line break is refused")
	}
}

func TestKeepDomain(t *testing.T) {
	list := []Share{{Name: "Fin",
		ShareACL: []ACE{{Principal: `ORIGEM\Financeiro`, Level: levelWrite}, {Principal: "Everyone", Level: levelRead}},
		NTFS: []ACE{{Principal: `OUTRO\Financeiro`, Level: levelWrite}, {Principal: `BUILTIN\Administrators`, Level: levelFull},
			{Principal: `OUTRO\Bloqueio`, Level: levelRead, Denied: true}}}}
	dropped := KeepDomain(list, "ORIGEM")
	if len(list[0].NTFS) != 2 || list[0].NTFS[0].Principal != `BUILTIN\Administrators` || !list[0].NTFS[1].Denied ||
		len(list[0].ShareACL) != 2 || !slices.Equal(dropped["fin"], []string{`OUTRO\Financeiro`}) {
		t.Fatalf("%+v dropped %v", list[0], dropped)
	}
	// without OUTRO\Financeiro on the NTFS side, ORIGEM\Financeiro gets nothing (the intersection is empty)
	p := Plan(list, Directory{NetBIOS: "ORIGEM", Users: map[string]bool{}}, map[string]string{`E:\`: "/srv/shares"})
	if slices.Contains(p[0].RW, `ORIGEM\Financeiro`) || !p[0].Denies {
		t.Fatalf("plan %+v", p[0])
	}
}

func TestParseCaclsKeepsEveryDeny(t *testing.T) {
	out := "ACL:ORIGEM\\Estagio:DENIED/0x0/0x00040000\nACL:ORIGEM\\Temp:DENIED/OI|CI|IO/READ\nACL:CREATOR OWNER:ALLOWED/OI|CI|IO/FULL\n"
	acl := parseCacls(out)
	if len(acl) != 2 || !acl[0].Denied || !acl[1].Denied {
		t.Fatalf("%+v", acl)
	}
}
