package picker

import (
	"strings"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// fuzzyMatch faz match por subsequencia (case-insensitive), como o filtro do
// fzf: todos os caracteres do padrao precisam aparecer na mesma ordem.
func fuzzyMatch(pattern, text string) bool {
	patternRunes := []rune(strings.ToLower(pattern))
	if len(patternRunes) == 0 {
		return true
	}
	index := 0
	for _, r := range strings.ToLower(text) {
		if r == patternRunes[index] {
			index++
			if index == len(patternRunes) {
				return true
			}
		}
	}
	return false
}

// rowMatches diz se a linha casa com o filtro digitado.
func rowMatches(row config.Row, filter string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}
	candidates := []string{
		row.Name,
		row.UserHost,
		row.Group,
		row.GroupLabel(),
		row.TagsCSV(),
	}
	for _, candidate := range candidates {
		if fuzzyMatch(filter, candidate) {
			return true
		}
	}
	return false
}
