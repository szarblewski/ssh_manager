// Package secrets guarda as senhas das conexoes.
//
// No lugar do secret-tool/libsecret do sshm.sh, o cofre do sistema e acessado
// pela API nativa de cada plataforma (Credential Manager no Windows, Keychain
// no macOS e Secret Service no Linux) atraves do github.com/zalando/go-keyring.
// Quando o cofre nao esta disponivel (ou o usuario pede --plain), a senha fica
// em texto puro no proprio hosts.json, como fallback documentado.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/szarblewski/ssh_manager/internal/config"
)

const (
	// Service e o nome do servico no cofre do sistema (igual ao sshm.sh).
	Service = "sshm"

	// EnvKeyring desliga o cofre quando vale 0, off, false ou no.
	EnvKeyring = "SSHM_KEYRING"
)

// ErrUnavailable indica que o cofre do sistema nao esta disponivel.
var ErrUnavailable = errors.New("cofre do sistema indisponivel")

// ErrNotFound indica que o segredo nao existe no cofre.
var ErrNotFound = keyring.ErrNotFound

// Backend diz onde a senha foi guardada.
type Backend int

const (
	// BackendNone indica que nenhuma senha foi guardada.
	BackendNone Backend = iota
	// BackendVault indica que a senha foi guardada no cofre do sistema.
	BackendVault
	// BackendPlain indica que a senha foi guardada em texto puro no arquivo.
	BackendPlain
)

// Vault e o cofre do sistema.
type Vault interface {
	Get(label string) (string, error)
	Set(label, secret string) error
	Delete(label string) error
	Description() string
}

// Enabled informa se o uso do cofre esta ligado (variavel SSHM_KEYRING).
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvKeyring))) {
	case "0", "off", "false", "no", "nao":
		return false
	default:
		return true
	}
}

// System devolve o cofre do sistema (ou um cofre desativado, se SSHM_KEYRING=off).
func System() Vault {
	if !Enabled() {
		return disabled{}
	}
	return keyringVault{}
}

// Available informa se o cofre aceita operacoes.
func Available(v Vault) bool {
	if _, ok := v.(disabled); ok {
		return false
	}
	return true
}

// Description descreve o cofre em uso (para mensagens ao usuario).
func Description(v Vault) string { return v.Description() }

type keyringVault struct{}

func (keyringVault) Get(label string) (string, error) {
	if label == "" {
		return "", ErrNotFound
	}
	secret, err := keyring.Get(Service, label)
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (keyringVault) Set(label, secret string) error {
	if label == "" {
		return fmt.Errorf("nome do segredo vazio")
	}
	return keyring.Set(Service, label, secret)
}

func (keyringVault) Delete(label string) error {
	if label == "" {
		return nil
	}
	err := keyring.Delete(Service, label)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (keyringVault) Description() string {
	switch runtime.GOOS {
	case "windows":
		return "Credential Manager do Windows"
	case "darwin":
		return "Keychain do macOS"
	default:
		return "cofre do sistema (Secret Service)"
	}
}

type disabled struct{}

func (disabled) Get(string) (string, error) { return "", ErrUnavailable }
func (disabled) Set(string, string) error   { return ErrUnavailable }
func (disabled) Delete(string) error        { return nil }
func (disabled) Description() string        { return "desativado (SSHM_KEYRING=off)" }

// Label devolve o nome do segredo de um host no cofre.
func Label(h config.Host) string {
	if h.PasswordSecret != "" {
		return h.PasswordSecret
	}
	return h.Name
}

// Lookup devolve a senha guardada do host (cofre ou texto puro) e os avisos a
// exibir. Nunca falha: sem senha guardada devolve "" e a conexao segue normal.
func Lookup(v Vault, h config.Host) (password string, warnings []string) {
	if h.Password == "" && h.PasswordSecret == "" {
		return "", nil
	}
	if h.PasswordSecret != "" {
		if !Available(v) {
			warnings = append(warnings, fmt.Sprintf(
				"aviso: '%s' tem senha no cofre, mas o uso do cofre esta desativado (%s=off).",
				h.Name, EnvKeyring))
		} else {
			secret, err := v.Get(h.PasswordSecret)
			switch {
			case err == nil && secret != "":
				return secret, nil
			case errors.Is(err, ErrNotFound):
				warnings = append(warnings, fmt.Sprintf(
					"aviso: senha de '%s' nao encontrada no %s; ela sera pedida no terminal.",
					h.PasswordSecret, v.Description()))
			case err != nil:
				warnings = append(warnings, fmt.Sprintf(
					"aviso: falha ao ler a senha de '%s' no %s (%v); ela sera pedida no terminal.",
					h.PasswordSecret, v.Description(), err))
			default:
				warnings = append(warnings, fmt.Sprintf(
					"aviso: senha de '%s' esta vazia no %s; ela sera pedida no terminal.",
					h.PasswordSecret, v.Description()))
			}
		}
		// senha em texto puro serve de reserva quando o cofre falha
		if h.Password != "" {
			return h.Password, warnings
		}
		return "", warnings
	}
	return h.Password, nil
}

// Store grava a senha e diz onde ela ficou. plain forca o texto puro no arquivo.
func Store(v Vault, label, password string, plain bool) (Backend, error) {
	if password == "" {
		return BackendNone, nil
	}
	if !plain && Available(v) {
		if err := v.Set(label, password); err != nil {
			return BackendNone, fmt.Errorf("falha ao gravar a senha no %s: %w", v.Description(), err)
		}
		return BackendVault, nil
	}
	return BackendPlain, nil
}

// Remove apaga a senha do cofre (o campo no arquivo e removido pelo chamador).
func Remove(v Vault, label string) error {
	if label == "" {
		return nil
	}
	if !Available(v) {
		return nil
	}
	if err := v.Delete(label); err != nil {
		return fmt.Errorf("falha ao apagar a senha de '%s' no %s: %w", label, v.Description(), err)
	}
	return nil
}

// MigrateResult resume a migracao de senhas em texto puro para o cofre.
type MigrateResult struct {
	Moved    int
	Failures []string
}

// MigrateToVault move as senhas em texto puro para o cofre do sistema.
//
// A senha so sai do arquivo depois de gravada no cofre; em caso de falha o host
// continua com a senha em texto puro e o erro vai para Failures.
func MigrateToVault(v Vault, cfg *config.Config) MigrateResult {
	var result MigrateResult
	for i := range cfg.Hosts {
		host := &cfg.Hosts[i]
		if host.Password == "" {
			continue
		}
		if host.Name == "" {
			result.Failures = append(result.Failures, "host sem nome ignorado")
			continue
		}
		if err := v.Set(host.Name, host.Password); err != nil {
			result.Failures = append(result.Failures,
				fmt.Sprintf("%s: falha ao gravar no %s: %v", host.Name, v.Description(), err))
			continue
		}
		host.Password = ""
		host.PasswordSecret = host.Name
		result.Moved++
	}
	return result
}
