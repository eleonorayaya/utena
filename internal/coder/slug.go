package coder

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const MaxSlugLen = 32

var (
	slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)
	slugValid   = regexp.MustCompile(`^[a-z]([a-z0-9]|-[a-z0-9])*$`)
)

func ValidSlug(s string) bool {
	return len(s) <= MaxSlugLen && slugValid.MatchString(s)
}

func baseSlug(name string) string {
	s := slugInvalid.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.TrimLeft(s, "-0123456789")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "session"
	}
	return truncateSlug(s, MaxSlugLen)
}

func truncateSlug(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.TrimRight(s, "-")
}

func Slug(name string, taken []string) (string, error) {
	used := make(map[string]struct{}, len(taken))
	for _, t := range taken {
		used[t] = struct{}{}
	}
	base := baseSlug(name)
	if _, ok := used[base]; !ok {
		return base, nil
	}
	for n := 2; n < 10000; n++ {
		suffix := "-" + strconv.Itoa(n)
		candidate := truncateSlug(base, MaxSlugLen-len(suffix)) + suffix
		if _, ok := used[candidate]; !ok {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free coder workspace name for %q", name)
}
