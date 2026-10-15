package config

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestHostRoundTripPreservesUnknownFields(t *testing.T) {
	raw := `{
      "id": 7,
      "name": "vps",
      "host": "10.0.0.1",
      "user": "root",
      "port": 2222,
      "group": "cliente-a",
      "tags": ["prod", "sap"],
      "password_secret": "vps",
      "algum_campo_novo": {"a": 1}
    }`

	var h Host
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if h.ID != 7 || h.Name != "vps" || h.Port != 2222 || h.Group != "cliente-a" {
		t.Fatalf("campos conhecidos lidos errado: %+v", h)
	}
	if len(h.Tags) != 2 || h.Tags[1] != "sap" {
		t.Fatalf("tags lidas errado: %+v", h.Tags)
	}
	if _, ok := h.Extra["algum_campo_novo"]; !ok {
		t.Fatalf("campo desconhecido nao preservado: %+v", h.Extra)
	}

	out, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "algum_campo_novo") {
		t.Fatalf("campo desconhecido perdido na escrita: %s", out)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("releitura: %v", err)
	}
	if round["algum_campo_novo"] == nil {
		t.Fatalf("campo desconhecido perdido: %v", round)
	}
}

func TestConfigRoundTripPreservesRootFields(t *testing.T) {
	raw := `{"hosts":[{"id":1,"name":"a","host":"h","user":"u"}],"versao":3}`

	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(c.Hosts) != 1 {
		t.Fatalf("hosts: %+v", c.Hosts)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"versao":3`) {
		t.Fatalf("chave raiz desconhecida perdida: %s", out)
	}
}

func TestEmptyHostsIsValid(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(`{}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Hosts == nil || len(c.Hosts) != 0 {
		t.Fatalf("hosts deveria ser lista vazia: %#v", c.Hosts)
	}
}

func TestNextIDAndNameExists(t *testing.T) {
	c := New()
	c.Hosts = []Host{{ID: 2, Name: "b"}, {ID: 9, Name: "a"}, {Name: "sem-id"}}
	if got := c.NextID(); got != 10 {
		t.Fatalf("NextID = %d, esperado 10", got)
	}
	if !c.NameExists("A") {
		t.Fatal("NameExists deveria ignorar maiusculas")
	}
	if c.NameExists("z") {
		t.Fatal("NameExists devolveu true para nome inexistente")
	}
}

func TestFind(t *testing.T) {
	c := New()
	c.Hosts = []Host{{ID: 1, Name: "alpha"}, {ID: 2, Name: "beta"}, {ID: 3, Name: "alpha"}}

	if idx, err := c.Find("1"); err != nil || idx != 0 {
		t.Fatalf("Find por id: idx=%d err=%v", idx, err)
	}
	if idx, err := c.Find("BETA"); err != nil || idx != 1 {
		t.Fatalf("Find por nome: idx=%d err=%v", idx, err)
	}
	if _, err := c.Find("nao-existe"); err == nil {
		t.Fatal("Find deveria falhar para chave inexistente")
	}
	if _, err := c.Find("alpha"); err == nil {
		t.Fatal("Find deveria falhar com nomes duplicados")
	}
	if got := c.FindAll("alpha"); len(got) != 2 {
		t.Fatalf("FindAll = %v, esperado 2 indices", got)
	}
}

func TestFilterRowsSortsByGroupThenName(t *testing.T) {
	c := New()
	c.Hosts = []Host{
		{ID: 1, Name: "zulu", Host: "h1", User: "u", Group: "Infra"},
		{ID: 2, Name: "alfa", Host: "h2", User: "u", Group: "cliente-a", Tags: []string{"prod"}},
		{ID: 3, Name: "beta", Host: "h3", User: "u", Tags: []string{"sozinho"}},
		{ID: 4, Name: "Alfa", Host: "h4", User: "u", Group: "cliente-a"},
	}

	rows := c.FilterRows("", "")
	got := []string{}
	for _, r := range rows {
		got = append(got, r.GroupLabel()+"/"+r.Name)
	}
	want := []string{"cliente-a/Alfa", "cliente-a/alfa", "Infra/zulu", "(sem grupo)/beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ordem = %v, esperado %v", got, want)
	}
	if !RowsHaveGroup(rows) {
		t.Fatal("RowsHaveGroup deveria ser true")
	}

	byGroup := c.FilterRows("cliente-a", "")
	if len(byGroup) != 2 {
		t.Fatalf("filtro por grupo = %d linhas", len(byGroup))
	}
	byTag := c.FilterRows("", "prod")
	if len(byTag) != 1 || byTag[0].Name != "alfa" {
		t.Fatalf("filtro por tag = %+v", byTag)
	}
	if got := c.FilterRows("cliente-a", "prod"); len(got) != 1 {
		t.Fatalf("filtros combinados = %+v", got)
	}
	if RowsHaveGroup(c.FilterRows("", "sozinho")) {
		t.Fatal("RowsHaveGroup deveria ser false para host sem grupo")
	}
}

func TestRowHelpers(t *testing.T) {
	r := Row{ID: 3, Name: "n", UserHost: "u@h", Port: 2222, Tags: []string{"a", "b"}}
	if r.GroupLabel() != NoGroupLabel {
		t.Fatalf("GroupLabel = %q", r.GroupLabel())
	}
	if r.TagsCSV() != "a,b" {
		t.Fatalf("TagsCSV = %q", r.TagsCSV())
	}
	if (Row{Group: "infra"}).GroupLabel() != "infra" {
		t.Fatal("GroupLabel com grupo deveria devolver o proprio grupo")
	}
}

func TestNormalizeTags(t *testing.T) {
	cases := map[string]string{
		" prod, sap ": "prod,sap",
		"a,,b":        "a,b",
		"  ":          "",
		"":            "",
		"um":          "um",
	}
	for in, want := range cases {
		if got := strings.Join(NormalizeTags(in), ","); got != want {
			t.Fatalf("NormalizeTags(%q) = %q, esperado %q", in, got, want)
		}
	}
}

func TestSanitizeNameAndValidatePort(t *testing.T) {
	if got := SanitizeName(" meu host "); got != "meuhost" {
		t.Fatalf("SanitizeName = %q", got)
	}
	if err := ValidatePort(0); err == nil {
		t.Fatal("ValidatePort(0) deveria falhar")
	}
	if err := ValidatePort(65536); err == nil {
		t.Fatal("ValidatePort(65536) deveria falhar")
	}
	if err := ValidatePort(22); err != nil {
		t.Fatalf("ValidatePort(22) = %v", err)
	}
}

func TestResolveIdentityFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(key, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveIdentityFile(key); got != key {
		t.Fatalf("ResolveIdentityFile = %q, esperado %q", got, key)
	}
	if got := ResolveIdentityFile(""); got != "" {
		t.Fatalf("ResolveIdentityFile(vazio) = %q", got)
	}
	missing := filepath.Join(dir, "nao-existe")
	if got := ResolveIdentityFile(missing); got != missing {
		t.Fatalf("caminho inexistente deveria voltar expandido: %q", got)
	}
}

func TestPathsCandidatesOrderAndDedup(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(EnvConfig, "") // ambiente limpo: sem caminho explicito

	candidates := Candidates("")
	if len(candidates) < 3 {
		t.Fatalf("candidatos insuficientes: %v", candidates)
	}
	if want := filepath.Join(appData, DirName, FileName); candidates[0] != want {
		t.Fatalf("primeiro candidato = %q, esperado %q", candidates[0], want)
	}
	seen := map[string]int{}
	for _, c := range candidates {
		if n := seen[strings.ToLower(c)]; n > 0 {
			t.Fatalf("candidato repetido: %q", c)
		}
		seen[strings.ToLower(c)]++
	}

	if got := CreatePath(""); got != filepath.Join(appData, DirName, FileName) {
		t.Fatalf("CreatePath = %q", got)
	}

	explicit := filepath.Join(home, "meu.json")
	t.Setenv(EnvConfig, explicit)
	if got := Candidates(""); len(got) != 1 || got[0] != explicit {
		t.Fatalf("SSHM_CONFIG deveria ser o unico candidato: %v", got)
	}
	if _, found := Resolve(""); found {
		t.Fatal("arquivo informado inexistente nao deveria ser encontrado")
	}
	if got := Candidates("outro.json"); len(got) != 1 || got[0] != "outro.json" {
		t.Fatalf("--config deveria ser o unico candidato: %v", got)
	}
	if got := CreatePath(""); got != explicit {
		t.Fatalf("CreatePath com SSHM_CONFIG = %q", got)
	}
}

// Um caminho informado nunca pode cair em outro arquivo (ex.: ./hosts.json):
// era um comportamento do sshm.sh e evita escrever na configuracao errada.
func TestExplicitPathNeverFallsBackToOtherFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", "")

	// hosts.json na pasta atual: seria encontrado se o caminho explicito vazasse
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(cwd, FileName)
	if _, err := os.Stat(fallback); err != nil {
		t.Skipf("sem %s para testar o vazamento", fallback)
	}

	explicit := filepath.Join(dir, "nao-existe.json")
	t.Setenv(EnvConfig, explicit)
	if _, found := Resolve(""); found {
		t.Fatal("com caminho explicito inexistente a busca nao deveria achar outro arquivo")
	}

	store, cfg, created, err := Open(explicit)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !created || store.Path != explicit {
		t.Fatalf("deveria criar %q, criou %q (created=%v)", explicit, store.Path, created)
	}
	if len(cfg.Hosts) != 0 {
		t.Fatalf("configuracao deveria comecar vazia: %+v", cfg.Hosts)
	}
}

func TestStoreRoundTripAndCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName)
	store := &Store{Path: path}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load em arquivo inexistente: %v", err)
	}
	if len(cfg.Hosts) != 0 {
		t.Fatalf("configuracao deveria estar vazia: %+v", cfg)
	}

	cfg.Hosts = append(cfg.Hosts, Host{ID: 1, Name: "a", Host: "h", User: "u", Tags: []string{"t"}})
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("arquivo temporario deveria ter sido removido")
	}

	again, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(again.Hosts) != 1 || again.Hosts[0].Name != "a" || again.Hosts[0].Tags[0] != "t" {
		t.Fatalf("round-trip: %+v", again.Hosts)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("arquivo deveria terminar com quebra de linha")
	}
	if !strings.Contains(string(data), "\"hosts\"") {
		t.Fatalf("conteudo inesperado: %s", data)
	}
}

func TestStoreSaveRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store := &Store{Path: path}
	if err := store.Save(nil); err == nil {
		t.Fatal("Save(nil) deveria falhar")
	}
}

func TestStoreOpenCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store, cfg, created, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !created {
		t.Fatal("created deveria ser true")
	}
	if store.Path != path {
		t.Fatalf("Path = %q", store.Path)
	}
	if len(cfg.Hosts) != 0 {
		t.Fatalf("configuracao deveria estar vazia: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("arquivo deveria existir: %v", err)
	}
}

func TestStoreSaveRestrictsPlaintextPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissao estilo POSIX nao se aplica ao Windows")
	}
	path := filepath.Join(t.TempDir(), FileName)
	store := &Store{Path: path}
	cfg := New()
	cfg.Hosts = append(cfg.Hosts, Host{ID: 1, Name: "a", Host: "h", User: "u", Password: "segredo"})
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissao = %v, esperado 0600", info.Mode().Perm())
	}
}

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SSHM_TESTVAR", "valor")

	if got := ExpandPath("~/chave"); got != filepath.Join(home, "chave") {
		t.Fatalf("ExpandPath(~) = %q", got)
	}
	if got := ExpandPath("$SSHM_TESTVAR/x"); got != filepath.Join("valor", "x") {
		t.Fatalf("ExpandPath($VAR) = %q", got)
	}
	if got := ExpandPath(""); got != "" {
		t.Fatalf("ExpandPath(vazio) = %q", got)
	}
}

// O Bloco de Notas e o PowerShell gravam BOM com facilidade; o encoding/json
// recusa um BOM no inicio, entao a leitura precisa normalizar.
func TestLoadToleratesUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := []byte("{\n  \"hosts\": [{\"id\": 1, \"name\": \"alfa\", \"host\": \"h\", \"user\": \"u\"}]\n}\n")
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := (&Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load com BOM UTF-8: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Name != "alfa" {
		t.Fatalf("hosts = %+v", cfg.Hosts)
	}
}

func TestLoadToleratesUTF16(t *testing.T) {
	for name, prefix := range map[string][]byte{
		"UTF-16LE": {0xFF, 0xFE},
		"UTF-16BE": {0xFE, 0xFF},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			encoded := encodeUTF16(`{"hosts":[{"id":9,"name":"beta","host":"h","user":"u"}]}`, prefix)
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := (&Store{Path: path}).Load()
			if err != nil {
				t.Fatalf("Load com %s: %v", name, err)
			}
			if len(cfg.Hosts) != 1 || cfg.Hosts[0].ID != 9 {
				t.Fatalf("hosts = %+v", cfg.Hosts)
			}
		})
	}
}

func TestNormalizeEncodingPassesThroughPlainUTF8(t *testing.T) {
	body := []byte(`{"hosts":[]}`)
	got, err := NormalizeEncoding(body)
	if err != nil {
		t.Fatalf("NormalizeEncoding: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("conteudo alterado: %q", got)
	}
}

// Caso relatado pelo usuario: o template vazio gravado pelo PowerShell nascia
// com BOM, e ate o arquivo vazio falhava.
func TestLoadToleratesBOMInEmptyFile(t *testing.T) {
	cases := map[string][]byte{
		"so o BOM":        {0xEF, 0xBB, 0xBF},
		"BOM e nada mais": append([]byte{0xEF, 0xBB, 0xBF}, []byte("\n")...),
		"BOM e espacos":   append([]byte{0xEF, 0xBB, 0xBF}, []byte("  \r\n\t")...),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := (&Store{Path: path}).Load()
			if err != nil {
				t.Fatalf("Load de arquivo vazio com BOM: %v", err)
			}
			if len(cfg.Hosts) != 0 {
				t.Fatalf("deveria estar vazio: %+v", cfg.Hosts)
			}
		})
	}
}

func TestDetectEncoding(t *testing.T) {
	cases := map[string]string{
		"UTF-8 com BOM":     string([]byte{0xEF, 0xBB, 0xBF}) + "{}",
		"UTF-16 LE com BOM": string([]byte{0xFF, 0xFE}) + "x\x00",
		"UTF-16 BE com BOM": string([]byte{0xFE, 0xFF}) + "\x00x",
		"UTF-8 sem BOM":     "{}",
	}
	for want, input := range cases {
		if got := DetectEncoding([]byte(input)); got != want {
			t.Fatalf("DetectEncoding = %q, esperado %q", got, want)
		}
	}
}

func TestReadFileReportsTruncatedUTF16(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte{0xFF, 0xFE, 0x7B, 0x00, 0x0A}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil {
		t.Fatal("UTF-16 truncado deveria falhar")
	}
}

func TestSaveWritesNoBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store := &Store{Path: path}
	cfg := New()
	cfg.Hosts = append(cfg.Hosts, Host{ID: 1, Name: "alfa", Host: "h", User: "u"})
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		t.Fatal("Save nao deveria gravar BOM")
	}
	// e o arquivo gravado e lido de volta sem problema
	if _, err := store.Load(); err != nil {
		t.Fatalf("Load apos Save: %v", err)
	}
}

// encodeUTF16 monta o arquivo UTF-16 com BOM a partir do prefixo informado.
func encodeUTF16(text string, bom []byte) []byte {
	units := utf16.Encode([]rune(text))
	var order binary.ByteOrder = binary.LittleEndian
	if bom[0] == 0xFE {
		order = binary.BigEndian
	}
	out := append([]byte(nil), bom...)
	for _, unit := range units {
		var pair [2]byte
		order.PutUint16(pair[:], unit)
		out = append(out, pair[:]...)
	}
	return out
}
