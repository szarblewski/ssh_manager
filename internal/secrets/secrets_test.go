package secrets

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/szarblewski/ssh_manager/internal/config"
)

func TestMain(m *testing.M) {
	// cofre em memoria: os testes nao tocam no Credential Manager real
	keyring.MockInit()
	m.Run()
}

func TestKeyringVaultSetGetDelete(t *testing.T) {
	v := System()
	if !Available(v) {
		t.Fatal("cofre deveria estar disponivel")
	}
	if err := v.Set("lab", "segredo"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := v.Get("lab")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "segredo" {
		t.Fatalf("Get = %q", got)
	}
	if err := v.Delete("lab"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := v.Get("lab"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get apos Delete = %v, esperado ErrNotFound", err)
	}
	if err := v.Delete("lab"); err != nil {
		t.Fatalf("Delete de segredo inexistente deveria ser silencioso: %v", err)
	}
	if _, err := v.Get(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(\"\") = %v", err)
	}
	if v.Description() == "" {
		t.Fatal("Description vazia")
	}
}

func TestEnabledRespectsEnv(t *testing.T) {
	t.Setenv(EnvKeyring, "")
	if !Enabled() {
		t.Fatal("cofre deveria estar ligado por padrao")
	}
	for _, off := range []string{"0", "off", "OFF", "false", "no", "nao"} {
		t.Setenv(EnvKeyring, off)
		if Enabled() {
			t.Fatalf("%q deveria desligar o cofre", off)
		}
		if Available(System()) {
			t.Fatalf("System() com %q deveria estar indisponivel", off)
		}
	}
	t.Setenv(EnvKeyring, "1")
	if !Enabled() || !Available(System()) {
		t.Fatal("cofre deveria estar ligado")
	}
}

func TestLabel(t *testing.T) {
	if got := Label(config.Host{Name: "vps", PasswordSecret: "cofre-vps"}); got != "cofre-vps" {
		t.Fatalf("Label = %q", got)
	}
	if got := Label(config.Host{Name: "vps"}); got != "vps" {
		t.Fatalf("Label = %q", got)
	}
}

func TestLookupPlaintext(t *testing.T) {
	v := System()
	password, warnings := Lookup(v, config.Host{Name: "a", Password: "123"})
	if password != "123" {
		t.Fatalf("password = %q", password)
	}
	if len(warnings) != 0 {
		t.Fatalf("avisos inesperados: %v", warnings)
	}

	password, warnings = Lookup(v, config.Host{Name: "a"})
	if password != "" || len(warnings) != 0 {
		t.Fatalf("host sem senha deveria devolver vazio: %q %v", password, warnings)
	}
}

func TestLookupFromVault(t *testing.T) {
	v := System()
	if err := v.Set("cofre-a", "do-cofre"); err != nil {
		t.Fatal(err)
	}
	password, warnings := Lookup(v, config.Host{Name: "a", PasswordSecret: "cofre-a"})
	if password != "do-cofre" {
		t.Fatalf("password = %q (avisos %v)", password, warnings)
	}
	if len(warnings) != 0 {
		t.Fatalf("avisos inesperados: %v", warnings)
	}
}

func TestLookupMissingSecretWarns(t *testing.T) {
	password, warnings := Lookup(System(), config.Host{Name: "a", PasswordSecret: "nao-existe"})
	if password != "" {
		t.Fatalf("password = %q", password)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "nao encontrada") {
		t.Fatalf("aviso esperado, obtido: %v", warnings)
	}
}

func TestLookupFallsBackToPlaintextWhenVaultFails(t *testing.T) {
	password, warnings := Lookup(failing{}, config.Host{Name: "a", PasswordSecret: "lbl", Password: "reserva"})
	if password != "reserva" {
		t.Fatalf("password = %q", password)
	}
	if len(warnings) == 0 {
		t.Fatal("deveria avisar sobre a falha no cofre")
	}
}

func TestLookupDisabledVault(t *testing.T) {
	t.Setenv(EnvKeyring, "off")
	password, warnings := Lookup(disabled{}, config.Host{Name: "a", PasswordSecret: "lbl"})
	if password != "" {
		t.Fatalf("password = %q", password)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "desativado") {
		t.Fatalf("aviso esperado, obtido: %v", warnings)
	}
}

func TestStoreBackends(t *testing.T) {
	v := System()
	backend, err := Store(v, "host-x", "senha", false)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if backend != BackendVault {
		t.Fatalf("backend = %v, esperado cofre", backend)
	}
	got, err := v.Get("host-x")
	if err != nil || got != "senha" {
		t.Fatalf("segredo no cofre = %q (%v)", got, err)
	}

	backend, err = Store(v, "host-y", "senha", true)
	if err != nil || backend != BackendPlain {
		t.Fatalf("backend com --plain = %v (%v)", backend, err)
	}

	backend, err = Store(v, "host-z", "", false)
	if err != nil || backend != BackendNone {
		t.Fatalf("senha vazia = %v (%v)", backend, err)
	}

	backend, err = Store(disabled{}, "host-w", "senha", false)
	if err != nil || backend != BackendPlain {
		t.Fatalf("cofre desativado deveria cair no texto puro: %v (%v)", backend, err)
	}
}

func TestStoreFailsWhenVaultErrors(t *testing.T) {
	if _, err := Store(failing{}, "host", "senha", false); err == nil {
		t.Fatal("Store deveria falhar quando o cofre falha")
	}
}

func TestRemove(t *testing.T) {
	v := System()
	if err := v.Set("host-rm", "x"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(v, "host-rm"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := v.Get("host-rm"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("segredo deveria ter sido apagado: %v", err)
	}
	if err := Remove(v, ""); err != nil {
		t.Fatalf("Remove(\"\") = %v", err)
	}
	if err := Remove(disabled{}, "qualquer"); err != nil {
		t.Fatalf("Remove com cofre desativado = %v", err)
	}
	if err := Remove(failing{}, "qualquer"); err == nil {
		t.Fatal("Remove deveria propagar a falha do cofre")
	}
}

func TestMigrateToVault(t *testing.T) {
	v := System()
	cfg := config.New()
	cfg.Hosts = []config.Host{
		{ID: 1, Name: "com-senha", Host: "h", User: "u", Password: "p1"},
		{ID: 2, Name: "sem-senha", Host: "h", User: "u"},
		{ID: 3, Name: "", Host: "h", User: "u", Password: "p3"},
		{ID: 4, Name: "ja-no-cofre", Host: "h", User: "u", PasswordSecret: "ja-no-cofre"},
	}

	result := MigrateToVault(v, cfg)
	if result.Moved != 1 {
		t.Fatalf("Moved = %d, esperado 1 (falhas: %v)", result.Moved, result.Failures)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0], "sem nome") {
		t.Fatalf("falhas = %v", result.Failures)
	}
	if cfg.Hosts[0].Password != "" || cfg.Hosts[0].PasswordSecret != "com-senha" {
		t.Fatalf("host migrado ficou errado: %+v", cfg.Hosts[0])
	}
	if cfg.Hosts[2].Password != "p3" {
		t.Fatal("host sem nome nao deveria perder a senha")
	}
	got, err := v.Get("com-senha")
	if err != nil || got != "p1" {
		t.Fatalf("senha no cofre = %q (%v)", got, err)
	}
}

func TestMigrateToVaultKeepsPlaintextOnFailure(t *testing.T) {
	cfg := config.New()
	cfg.Hosts = []config.Host{{ID: 1, Name: "a", Host: "h", User: "u", Password: "p1"}}

	result := MigrateToVault(failing{}, cfg)
	if result.Moved != 0 || len(result.Failures) != 1 {
		t.Fatalf("resultado inesperado: %+v", result)
	}
	if cfg.Hosts[0].Password != "p1" || cfg.Hosts[0].PasswordSecret != "" {
		t.Fatalf("senha em texto puro deveria continuar: %+v", cfg.Hosts[0])
	}
}

type failing struct{}

func (failing) Get(string) (string, error) { return "", errors.New("cofre fora do ar") }
func (failing) Set(string, string) error   { return errors.New("cofre fora do ar") }
func (failing) Delete(string) error        { return errors.New("cofre fora do ar") }
func (failing) Description() string        { return "cofre de teste" }
