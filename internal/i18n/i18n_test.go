package i18n

import (
	"regexp"
	"testing"
)

func TestT(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"invalid user or password", "usuário ou senha inválidos"},
		{"access denied: domain administrators only", "acesso negado: apenas administradores do domínio"},
		{`directory unavailable: LDAP Result Code 200 "Network Error"`, `diretório indisponível: LDAP Result Code 200 "Network Error"`},
		{"invalid data: OU name", "dados inválidos: nome da OU"},
		{`invalid input: group "EMPRESA\Financeiro"`, `entrada inválida: grupo "EMPRESA\Financeiro"`},
		{`invalid input: share name "a/b" (letters, numbers, space . _ -; reserved: global, homes)`,
			`entrada inválida: nome de compartilhamento "a/b" (letras, números, espaço . _ -; reservados: global, homes)`},
		{`invalid input: "/srv/x" is outside the allowed roots [/srv/shares]`, `entrada inválida: "/srv/x" fora das raízes permitidas [/srv/shares]`},
		{"member ana: group or member missing", "membro ana: grupo ou membro inexistente"},
		{"enabled: yes → no", "ativo: sim → não"},
		{`description: "a" → "b"`, `descrição: "a" → "b"`},
		{"Ana Souza in OU=Vendas", "Ana Souza em OU=Vendas"},
		{"server fs1 unavailable: dial tcp: refused", "servidor fs1 indisponível: dial tcp: refused"},
		{"connect to ldaps://dc1: x509: bad", "conectar em ldaps://dc1: x509: bad"},
		{"queued (abc)", "na fila (abc)"},
		{"invalid data: theme: default or admanager", "dados inválidos: tema: default ou admanager"},
		{"theme: default, admanager or graphite", "tema: default, admanager ou graphite"},
		{"user or password refused: couldn't connect SMB: logon failure", "usuário ou senha recusados: couldn't connect SMB: logon failure"},
		{"the server name does not resolve (check this server's DNS, or use the IP with user and password)", "o nome do servidor não resolve (confira o DNS deste servidor, ou use o IP com usuário e senha)"},
		{"an earlier run left the Windows inheritance pending: reapplying it to the whole folder",
			"uma execução anterior deixou a herança do Windows pendente: reaplicando-a na pasta inteira"},
		{"incremental Windows inheritance failed (samba bridge: no answer (i/o timeout)), reapplying to the whole folder",
			"a herança incremental do Windows falhou (samba bridge: no answer (i/o timeout)), reaplicando na pasta inteira"},
		{"could not mark the Windows inheritance pending: open x: no space left on device",
			"não foi possível marcar a herança do Windows como pendente: open x: no space left on device"},
		{"server fs1 unavailable: the answer is larger than 4 MiB", "servidor fs1 indisponível: a resposta é maior que 4 MiB"},
		{"something new", "something new"},
	}
	for _, tt := range tests {
		if got := T("pt-BR", tt.in); got != tt.want {
			t.Errorf("T(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := T("en", "invalid data"); got != "invalid data" {
		t.Errorf("English must stay unchanged, got %q", got)
	}
}

// TestPatternsPortable keeps the patterns in the subset that the web panel
// (JavaScript) also understands.
func TestPatternsPortable(t *testing.T) {
	t.Parallel()
	bad := regexp.MustCompile(`\(\?[^:]|\\[pPzAQE]|\[\[:`)
	for _, p := range ptPatterns {
		if bad.MatchString(p.re) {
			t.Errorf("pattern %q uses syntax that differs in JavaScript", p.re)
		}
	}
}

func TestShareDetailPT(t *testing.T) {
	got := T("pt-BR", `/srv/shares/Fin — full control: D\Domain Admins; write: D\Domain Users; read: D\Todos`)
	if want := `/srv/shares/Fin — controle total: D\Domain Admins; escrita: D\Domain Users; leitura: D\Todos`; got != want {
		t.Fatalf("%q", got)
	}
}
