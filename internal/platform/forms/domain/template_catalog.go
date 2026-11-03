package domain

// ValidTemplateCode shares the schema definition's exact code grammar with
// catalog lookups; empty or unbounded arbitrary query strings are not codes.
func ValidTemplateCode(code string) bool { return reCode.MatchString(code) }
