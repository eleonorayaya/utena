package bundles

import (
	"encoding/json"
	"maps"
	"slices"
)

type Bundle struct {
	Plugins    []string `json:"plugins,omitempty"`
	McpServers []string `json:"mcp_servers,omitempty"`
}

type Catalog map[string]Bundle

type Servers map[string]json.RawMessage

type Entry struct {
	Name       string   `json:"name"`
	Plugins    []string `json:"plugins"`
	McpServers []string `json:"mcp_servers"`
}

func (c Catalog) ManagedPlugins() []string {
	return c.managed(func(b Bundle) []string { return b.Plugins })
}

func (c Catalog) ManagedServers() []string {
	return c.managed(func(b Bundle) []string { return b.McpServers })
}

func (c Catalog) Resolve(names []string) ([]string, []string) {
	return c.resolve(names, func(b Bundle) []string { return b.Plugins })
}

func (c Catalog) ResolveServers(names []string) ([]string, []string) {
	return c.resolve(names, func(b Bundle) []string { return b.McpServers })
}

func (c Catalog) managed(field func(Bundle) []string) []string {
	set := make(map[string]struct{})
	for _, b := range c {
		for _, v := range field(b) {
			set[v] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func (c Catalog) resolve(names []string, field func(Bundle) []string) ([]string, []string) {
	set := make(map[string]struct{})
	var unknown []string
	for _, name := range names {
		b, ok := c[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		for _, v := range field(b) {
			set[v] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set)), unknown
}

func (c Catalog) Entries() []Entry {
	entries := make([]Entry, 0, len(c))
	for _, name := range slices.Sorted(maps.Keys(c)) {
		entries = append(entries, Entry{
			Name:       name,
			Plugins:    append([]string{}, c[name].Plugins...),
			McpServers: append([]string{}, c[name].McpServers...),
		})
	}
	return entries
}

func Union(lists ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, list := range lists {
		for _, v := range list {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
