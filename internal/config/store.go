package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Store le e grava o arquivo hosts.json mantendo a gravacao atomica e a
// permissao restrita quando ha senha em texto puro.
type Store struct {
	Path string
}

// Open localiza (ou escolhe) o arquivo de configuracao e devolve o store com o
// conteudo carregado. created=true quando um arquivo vazio acabou de ser criado.
func Open(explicit string) (store *Store, cfg *Config, created bool, err error) {
	path, found := Resolve(explicit)
	if !found {
		path = CreatePath(explicit)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			created = true
		}
	}
	store = &Store{Path: path}
	cfg, err = store.Load()
	if err != nil {
		return store, nil, created, err
	}
	if created {
		if err := store.Save(cfg); err != nil {
			return store, nil, false, err
		}
	}
	return store, cfg, created, nil
}

// Load le o arquivo de configuracao. Arquivo inexistente ou vazio devolve uma
// configuracao vazia. BOM (UTF-8/UTF-16) e tolerado, veja ReadFile.
func (s *Store) Load() (*Config, error) {
	data, err := ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("nao foi possivel ler %s: %w", s.Path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return New(), nil
	}
	cfg := New()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("JSON invalido em %s: %w", s.Path, err)
	}
	return cfg, nil
}

// Save grava a configuracao de forma atomica: escreve em um arquivo temporario
// na mesma pasta, valida o JSON e so entao substitui o arquivo original.
func (s *Store) Save(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("configuracao vazia")
	}
	if cfg.Hosts == nil {
		cfg.Hosts = []Host{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao gerar o JSON: %w", err)
	}
	data = append(data, '\n')

	// valida antes de substituir o arquivo bom
	var check Config
	if err := json.Unmarshal(data, &check); err != nil {
		return fmt.Errorf("falha ao gerar o JSON; %s nao foi alterado: %w", s.Path, err)
	}

	dir := filepath.Dir(s.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("nao foi possivel criar o diretorio %s: %w", dir, err)
		}
	}

	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("nao foi possivel escrever em %s: %w", tmp, err)
	}
	if err := renameWithRetry(tmp, s.Path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("nao foi possivel atualizar %s: %w", s.Path, err)
	}
	if HasPlaintextPassword(cfg) && runtime.GOOS != "windows" {
		if err := os.Chmod(s.Path, 0o600); err != nil {
			return fmt.Errorf("nao foi possivel restringir a permissao de %s: %w", s.Path, err)
		}
	}
	return nil
}

// renameWithRetry troca o arquivo, repetindo em caso de bloqueio temporario
// (antivirus/indexador no Windows costumam segurar o arquivo por instantes).
func renameWithRetry(from, to string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	return err
}

// HasPlaintextPassword diz se algum host guarda senha em texto puro no arquivo.
func HasPlaintextPassword(cfg *Config) bool {
	for _, h := range cfg.Hosts {
		if h.Password != "" {
			return true
		}
	}
	return false
}
