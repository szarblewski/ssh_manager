package config

import (
	"sort"
	"strings"
)

// NoGroupLabel e o rotulo usado na listagem para hosts sem grupo.
const NoGroupLabel = "(sem grupo)"

// Row e uma linha da listagem/selecao.
type Row struct {
	ID       int
	Name     string
	UserHost string
	Port     int
	Group    string
	Tags     []string
}

// GroupLabel devolve o grupo para exibicao ("(sem grupo)" quando vazio).
func (r Row) GroupLabel() string {
	if r.Group == "" {
		return NoGroupLabel
	}
	return r.Group
}

// TagsCSV devolve as tags separadas por virgula.
func (r Row) TagsCSV() string { return strings.Join(r.Tags, ",") }

// FilterRows devolve as linhas que casam com os filtros, ordenadas por grupo
// (hosts sem grupo no fim) e depois por nome, como no sshm.sh.
func (c *Config) FilterRows(groupFilter, tagFilter string) []Row {
	rows := make([]Row, 0, len(c.Hosts))
	for _, h := range c.Hosts {
		if groupFilter != "" && h.Group != groupFilter {
			continue
		}
		if tagFilter != "" && !containsTag(h.Tags, tagFilter) {
			continue
		}
		rows = append(rows, Row{
			ID:       h.ID,
			Name:     h.Name,
			UserHost: h.Target(),
			Port:     h.PortOrDefault(),
			Group:    h.Group,
			Tags:     append([]string(nil), h.Tags...),
		})
	}
	SortRows(rows)
	return rows
}

// SortRows ordena por grupo (alfabetico, sem grupo no fim) e depois por nome.
func SortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		gi, gj := rows[i].Group, rows[j].Group
		if gi != gj {
			if gi == "" {
				return false
			}
			if gj == "" {
				return true
			}
			li, lj := strings.ToLower(gi), strings.ToLower(gj)
			if li != lj {
				return li < lj
			}
			return gi < gj
		}
		ni, nj := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
		if ni != nj {
			return ni < nj
		}
		return rows[i].Name < rows[j].Name
	})
}

// RowsHaveGroup diz se alguma linha tem grupo (decide o agrupamento da listagem).
func RowsHaveGroup(rows []Row) bool {
	for _, r := range rows {
		if r.Group != "" {
			return true
		}
	}
	return false
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// NormalizeTags limpa uma lista de tags separada por virgula: remove espacos,
// itens vazios e virgulas sobrando (mesmo comportamento do sshm.sh).
func NormalizeTags(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
