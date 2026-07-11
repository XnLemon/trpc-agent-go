//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"regexp"
	"strings"
)

var defaultRedactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(Authorization:\s*(?:Basic|Bearer)\s+)[A-Za-z0-9._~+/\-]+=*`),
	regexp.MustCompile(`(?i)(Authorization\s*=\s*Bearer\s+)[^\r\n\s]+`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/\-]+=*`),
	regexp.MustCompile(`(?im)(authorization\s*:\s*(?:token|digest)\s+)[^\r\n]+`),
	regexp.MustCompile(`(?im)(authorization\s*=\s*(?:token|digest)\s+)[^\r\n]+`),
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|cookie)\s*=\s*([^&\s]+)`),
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|cookie)\s*:\s*([^,\s]+)`),
	regexp.MustCompile(`(?i)("(?:api[_-]?key|token|secret|password|passwd|authorization|cookie)"\s*:\s*")([^"]+)(")`),
	regexp.MustCompile(`(?i)(sk-[A-Za-z0-9._~+/\-]{8,})`),
	regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s/?#]*@[^\s/?#]+`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
}

// Redactor masks sensitive values before logging, tracing, or auditing.
type Redactor struct {
	patterns []*regexp.Regexp
}

// NewRedactor returns a redactor with default secret patterns and optional extras.
func NewRedactor(extraPatterns ...string) (*Redactor, error) {
	patterns := append([]*regexp.Regexp(nil), defaultRedactionPatterns...)
	for _, pattern := range extraPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, compiled)
	}
	return &Redactor{patterns: patterns}, nil
}

// Redact returns text with known sensitive values masked.
func (r *Redactor) Redact(text string) string {
	if r == nil {
		r, _ = NewRedactor()
	}
	redacted := text
	for _, pattern := range r.patterns {
		redacted = pattern.ReplaceAllStringFunc(redacted, redactMatch)
	}
	return redacted
}

func redactMatch(match string) string {
	lower := strings.ToLower(match)
	if strings.Contains(lower, "authorization:") {
		if idx := strings.Index(match, ":"); idx >= 0 {
			return match[:idx+1] + " ****"
		}
	}
	if strings.Contains(lower, "authorization=") {
		if idx := strings.Index(match, "="); idx >= 0 {
			return match[:idx+1] + "****"
		}
	}
	if strings.Contains(lower, "bearer ") {
		return match[:strings.Index(lower, "bearer ")+7] + "****"
	}
	if strings.Contains(match, "://") && strings.Contains(match, "@") {
		if redacted, ok := redactURLUserinfo(match); ok {
			return redacted
		}
	}
	if strings.HasPrefix(match, "-----BEGIN ") {
		return "-----BEGIN PRIVATE KEY-----****-----END PRIVATE KEY-----"
	}
	if idx := strings.Index(match, "="); idx >= 0 {
		return match[:idx+1] + "****"
	}
	if idx := strings.Index(match, ":"); idx >= 0 {
		prefix := match[:idx+1]
		rest := match[idx+1:]
		if strings.HasPrefix(strings.TrimLeft(rest, " \t"), "\"") && strings.HasSuffix(match, "\"") {
			return prefix + " \"****\""
		}
		return prefix + " ****"
	}
	if len(match) <= 8 {
		return "****"
	}
	return match[:4] + "****" + match[len(match)-4:]
}

func redactURLUserinfo(match string) (string, bool) {
	scheme := strings.Index(match, "://")
	if scheme < 0 {
		return match, false
	}
	authorityStart := scheme + len("://")
	authorityEnd := len(match)
	if end := strings.IndexAny(match[authorityStart:], "/?# \t\r\n"); end >= 0 {
		authorityEnd = authorityStart + end
	}
	at := strings.LastIndex(match[authorityStart:authorityEnd], "@")
	if at <= 0 {
		return match, false
	}
	at += authorityStart
	return match[:authorityStart] + "****" + match[at:], true
}
