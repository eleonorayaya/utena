package bundles

import (
	"maps"
	"slices"
)

type Bundle struct {
	Plugins []string `json:"plugins"`
}

type Catalog map[string]Bundle

type Entry struct {
	Name    string   `json:"name"`
	Plugins []string `json:"plugins"`
}

func (c Catalog) ManagedPlugins() []string {
	set := make(map[string]struct{})
	for _, b := range c {
		for _, p := range b.Plugins {
			set[p] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func (c Catalog) Resolve(names []string) ([]string, []string) {
	set := make(map[string]struct{})
	var unknown []string
	for _, name := range names {
		b, ok := c[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		for _, p := range b.Plugins {
			set[p] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set)), unknown
}

func (c Catalog) Entries() []Entry {
	entries := make([]Entry, 0, len(c))
	for _, name := range slices.Sorted(maps.Keys(c)) {
		entries = append(entries, Entry{Name: name, Plugins: c[name].Plugins})
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
