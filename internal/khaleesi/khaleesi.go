package khaleesi

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

type Khaleesi struct {
	re   *regexp.Regexp
	subs map[string][]string
}

func New() (*Khaleesi, error) {
	kh := Khaleesi{
		subs: Replaces(),
	}

	expr := strings.Join(Keys(kh.subs), "|") + "gi"

	var err error
	kh.re, err = regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("compile regexp: %w", err)
	}

	return &kh, nil
}

func (k *Khaleesi) Modify(input string) (string, bool) {
	output := k.re.ReplaceAllStringFunc(input, func(s string) string {
		ss := k.subs[s]
		idx := rand.Intn(len(ss))
		return ss[idx]
	})

	return output, input != output
}
