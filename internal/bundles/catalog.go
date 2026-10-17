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

func (c Catalog) Resolve(names []string) (plugins, servers, unknown []string) {
	pluginSet := make(map[string]struct{})
	serverSet := make(map[string]struct{})
	for _, name := range names {
		b, ok := c[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		for _, p := range b.Plugins {
			pluginSet[p] = struct{}{}
		}
		for _, sv := range b.McpServers {
			serverSet[sv] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(pluginSet)), slices.Sorted(maps.Keys(serverSet)), unknown
}

func (c Catalog) Managed() (plugins, servers []string) {
	pluginSet := make(map[string]struct{})
	serverSet := make(map[string]struct{})
	for _, b := range c {
		for _, p := range b.Plugins {
			pluginSet[p] = struct{}{}
		}
		for _, sv := range b.McpServers {
			serverSet[sv] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(pluginSet)), slices.Sorted(maps.Keys(serverSet))
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
