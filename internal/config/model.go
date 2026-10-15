// Package config carrega, valida e grava o hosts.json do sshm.
//
// O formato e o mesmo do sshm.sh (bash/jq), para que o mesmo arquivo continue
// valendo nas duas versoes e entre sistemas operacionais. Campos desconhecidos
// sao preservados em Extra: um arquivo escrito por outra ferramenta (ou por uma
// versao futura) nunca e reescrito perdendo dados.
package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DefaultPort e a porta usada quando o host nao define "port".
const DefaultPort = 22

// hostFields lista as chaves que o sshm conhece em cada item de "hosts".
var hostFields = map[string]bool{
	"id":              true,
	"name":            true,
	"host":            true,
	"user":            true,
	"port":            true,
	"identity_file":   true,
	"group":           true,
	"tags":            true,
	"password":        true,
	"password_secret": true,
	"proxy_jump":      true,
	"connect_timeout": true,
}

// configFields lista as chaves conhecidas no nivel raiz do arquivo.
var configFields = map[string]bool{"hosts": true}

// Host e uma conexao cadastrada.
type Host struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	Host           string   `json:"host"`
	User           string   `json:"user"`
	Port           int      `json:"port,omitempty"`
	IdentityFile   string   `json:"identity_file,omitempty"`
	Group          string   `json:"group,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Password       string   `json:"password,omitempty"`
	PasswordSecret string   `json:"password_secret,omitempty"`
	ProxyJump      string   `json:"proxy_jump,omitempty"`
	ConnectTimeout int      `json:"connect_timeout,omitempty"`

	// Extra guarda os campos JSON que o sshm nao conhece (round-trip fiel).
	Extra map[string]json.RawMessage `json:"-"`
}

// PortOrDefault devolve a porta configurada ou DefaultPort.
func (h Host) PortOrDefault() int {
	if h.Port <= 0 {
		return DefaultPort
	}
	return h.Port
}

// Target devolve "usuario@host" como o sshm.sh exibe.
func (h Host) Target() string { return h.User + "@" + h.Host }

// Address devolve "host:porta" para o dial.
func (h Host) Address() string { return fmt.Sprintf("%s:%d", h.Host, h.PortOrDefault()) }

// HasPassword diz se ha senha guardada (texto puro ou cofre) para este host.
func (h Host) HasPassword() bool {
	return h.Password != "" || h.PasswordSecret != ""
}

// Clone devolve uma copia independente do host (Tags e Extra inclusos).
func (h Host) Clone() Host {
	out := h
	if h.Tags != nil {
		out.Tags = append([]string(nil), h.Tags...)
	}
	if h.Extra != nil {
		out.Extra = make(map[string]json.RawMessage, len(h.Extra))
		for k, v := range h.Extra {
			out.Extra[k] = v
		}
	}
	return out
}

// UnmarshalJSON le os campos conhecidos e guarda o resto em Extra.
func (h *Host) UnmarshalJSON(data []byte) error {
	type plain Host // sem os metodos de json.Marshaler/Unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k := range hostFields {
		delete(raw, k)
	}
	*h = Host(p)
	// campos vazios nao contam como dado extra
	for k := range raw {
		if string(raw[k]) == "null" {
			delete(raw, k)
		}
	}
	if len(raw) > 0 {
		h.Extra = raw
	}
	return nil
}

// MarshalJSON grava os campos conhecidos e reescreve os desconhecidos.
func (h Host) MarshalJSON() ([]byte, error) {
	type plain Host
	encoded, err := json.Marshal(plain(h))
	if err != nil {
		return nil, err
	}
	known := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &known); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(known)+len(h.Extra))
	for k, v := range h.Extra {
		out[k] = v
	}
	for k, v := range known {
		out[k] = v
	}
	return json.Marshal(out)
}

// Config e o conteudo do arquivo hosts.json.
type Config struct {
	Hosts []Host `json:"hosts"`

	// Extra guarda chaves desconhecidas no nivel raiz.
	Extra map[string]json.RawMessage `json:"-"`
}

// New devolve uma configuracao vazia pronta para uso.
func New() *Config { return &Config{Hosts: []Host{}} }

// UnmarshalJSON preserva as chaves desconhecidas no nivel raiz.
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k := range configFields {
		delete(raw, k)
	}
	for k := range raw {
		if string(raw[k]) == "null" {
			delete(raw, k)
		}
	}
	*c = Config(p)
	if c.Hosts == nil {
		c.Hosts = []Host{}
	}
	if len(raw) > 0 {
		c.Extra = raw
	}
	return nil
}

// MarshalJSON grava "hosts" junto com as chaves desconhecidas.
func (c Config) MarshalJSON() ([]byte, error) {
	type plain Config
	encoded, err := json.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	known := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &known); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(known)+len(c.Extra))
	for k, v := range c.Extra {
		out[k] = v
	}
	for k, v := range known {
		out[k] = v
	}
	return json.Marshal(out)
}

// NextID devolve o proximo id livre (maior id + 1), como no sshm.sh.
func (c *Config) NextID() int {
	max := 0
	for _, h := range c.Hosts {
		if h.ID > max {
			max = h.ID
		}
	}
	return max + 1
}

// NameExists diz se ja existe um host com esse nome (ignorando maiusculas).
func (c *Config) NameExists(name string) bool {
	return len(c.FindByName(name)) > 0
}

// FindByName devolve os indices cujo nome bate (case-insensitive).
func (c *Config) FindByName(name string) []int {
	var out []int
	for i, h := range c.Hosts {
		if strings.EqualFold(h.Name, name) {
			out = append(out, i)
		}
	}
	return out
}

// Find localiza um host por id numerico ou por nome.
//
// Devolve ErrNotFound quando nada casa e ErrAmbiguous quando mais de um host
// casa com a chave informada.
func (c *Config) Find(key string) (int, error) {
	indices := c.FindAll(key)
	switch len(indices) {
	case 0:
		return -1, fmt.Errorf("%w: %s", ErrNotFound, key)
	case 1:
		return indices[0], nil
	default:
		return -1, fmt.Errorf("%w: %s", ErrAmbiguous, key)
	}
}

// FindAll devolve os indices que casam com a chave (id numerico ou nome).
func (c *Config) FindAll(key string) []int {
	if id, ok := parseID(key); ok {
		var out []int
		for i, h := range c.Hosts {
			if h.ID == id {
				out = append(out, i)
			}
		}
		return out
	}
	return c.FindByName(key)
}

// GroupExists diz se algum host esta no grupo informado.
func (c *Config) GroupExists(group string) bool {
	for _, h := range c.Hosts {
		if h.Group == group {
			return true
		}
	}
	return false
}

// Groups devolve a lista ordenada de grupos existentes.
func (c *Config) Groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range c.Hosts {
		if h.Group == "" || seen[h.Group] {
			continue
		}
		seen[h.Group] = true
		out = append(out, h.Group)
	}
	sort.Strings(out)
	return out
}

func parseID(key string) (int, bool) {
	if key == "" {
		return 0, false
	}
	n := 0
	for _, r := range key {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}
