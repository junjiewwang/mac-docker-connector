package main

import (
	"sort"
	"strings"
)

func cleanIptablesToken(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), "\"")
}

func tokenizeIptablesLine(line string) []string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens = append(tokens, cleanIptablesToken(field))
	}
	return tokens
}

func canonicalizeIptablesArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := cleanIptablesToken(args[i])
		switch arg {
		case "-m":
			if i+1 >= len(args) {
				normalized = append(normalized, arg)
				continue
			}
			module := cleanIptablesToken(args[i+1])
			i++
			if module == "comment" {
				continue
			}
			normalized = append(normalized, "-m="+module)
		case "--comment":
			if i+1 < len(args) {
				i++
			}
			continue
		case "-i", "-o", "-s", "-d", "-j", "--ctstate":
			if i+1 >= len(args) {
				normalized = append(normalized, arg)
				continue
			}
			normalized = append(normalized, arg+"="+cleanIptablesToken(args[i+1]))
			i++
		default:
			normalized = append(normalized, arg)
		}
	}
	sort.Strings(normalized)
	return normalized
}

func iptablesRuleSignature(chain string, args []string) string {
	chain = cleanIptablesToken(chain)
	if chain == "" {
		return ""
	}
	return chain + "|" + strings.Join(canonicalizeIptablesArgs(args), "|")
}

func iptablesLineSignature(line string) string {
	fields := tokenizeIptablesLine(line)
	if len(fields) < 3 {
		return ""
	}
	switch fields[0] {
	case "-A", "-D", "-C":
		return iptablesRuleSignature(fields[1], fields[2:])
	default:
		return ""
	}
}
