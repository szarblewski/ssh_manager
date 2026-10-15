package config

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"unicode/utf16"
)

// Marcas de ordem de bytes (BOM) que editores do Windows costumam gravar.
var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// ReadFile le o arquivo de configuracao devolvendo UTF-8 sem BOM.
//
// O Bloco de Notas e o PowerShell (Set-Content/Out-File) gravam BOM com
// facilidade - e o encoding/json recusa um BOM no inicio - entao a leitura
// normaliza UTF-8 com BOM e UTF-16 (LE/BE, com BOM) para UTF-8 puro.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return NormalizeEncoding(data)
}

// NormalizeEncoding devolve o conteudo em UTF-8 sem BOM.
func NormalizeEncoding(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		return data[len(bomUTF8):], nil
	case bytes.HasPrefix(data, bomUTF16LE):
		return utf16ToUTF8(data[len(bomUTF16LE):], binary.LittleEndian)
	case bytes.HasPrefix(data, bomUTF16BE):
		return utf16ToUTF8(data[len(bomUTF16BE):], binary.BigEndian)
	default:
		return data, nil
	}
}

// DetectEncoding descreve a codificacao do arquivo (para mensagens ao usuario).
func DetectEncoding(data []byte) string {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		return "UTF-8 com BOM"
	case bytes.HasPrefix(data, bomUTF16LE):
		return "UTF-16 LE com BOM"
	case bytes.HasPrefix(data, bomUTF16BE):
		return "UTF-16 BE com BOM"
	default:
		return "UTF-8 sem BOM"
	}
}

func utf16ToUTF8(data []byte, order binary.ByteOrder) ([]byte, error) {
	if len(data)%2 != 0 {
		return nil, errors.New("arquivo UTF-16 truncado (numero impar de bytes)")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[i*2 : i*2+2])
	}
	return []byte(string(utf16.Decode(units))), nil
}
