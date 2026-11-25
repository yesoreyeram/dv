package httpsource

import "strings"

// nextLink returns the target of the rel="next" link in RFC 8288 Link header values, or
// "" (FR-072 AC5). Relation types match case-insensitively among space-separated
// relations; a results="false" attribute (Sentry) means there is no next page.
func nextLink(values []string) string {
	for _, v := range values {
		for _, link := range splitLinks(v) {
			target, params, ok := parseLink(link)
			if !ok {
				continue
			}
			rels := strings.Fields(strings.ToLower(params["rel"]))
			isNext := false
			for _, r := range rels {
				if r == "next" {
					isNext = true
				}
			}
			if !isNext {
				continue
			}
			if strings.EqualFold(params["results"], "false") {
				return ""
			}
			return target
		}
	}
	return ""
}

// splitLinks splits a Link header value on commas outside <...> and quoted strings.
func splitLinks(v string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '"' && depth == 0:
			quoted = !quoted
		case c == '<' && !quoted:
			depth++
		case c == '>' && !quoted && depth > 0:
			depth--
		case c == ',' && depth == 0 && !quoted:
			out = append(out, v[start:i])
			start = i + 1
		}
	}
	return append(out, v[start:])
}

func parseLink(s string) (string, map[string]string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return "", nil, false
	}
	end := strings.IndexByte(s, '>')
	if end < 0 {
		return "", nil, false
	}
	target := s[1:end]
	params := map[string]string{}
	for _, part := range strings.Split(s[end+1:], ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, val, _ := strings.Cut(part, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if _, dup := params[k]; !dup {
			params[k] = val
		}
	}
	return target, params, true
}
