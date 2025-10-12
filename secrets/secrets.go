package secrets

import (
	"regexp"
	"strings"
	"sync"
)

var secretReplacement = strings.Repeat("*", 4)

func Hide(s string, secrets []string) string {
	for _, secret := range secrets {
		re := compileStringToRegex(secret)
		s = re.ReplaceAllString(s, secretReplacement)
	}
	return s
}

var regexCache sync.Map

func compileStringToRegex(s string) *regexp.Regexp {
	if cached, ok := regexCache.Load(s); ok {
		return cached.(*regexp.Regexp)
	}

	re := regexp.MustCompile(regexp.QuoteMeta(s))
	regexCache.Store(s, re)
	return re
}
