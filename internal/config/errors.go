package config

import "errors"

var (
	// ErrNotFound indica que nenhum host casou com a chave informada.
	ErrNotFound = errors.New("nenhum host encontrado")

	// ErrAmbiguous indica que mais de um host casou com a chave informada.
	ErrAmbiguous = errors.New("mais de um host casou com a chave")
)
