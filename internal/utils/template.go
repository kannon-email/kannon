package utils

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

func ReplaceCustomFields(str string, fields map[string]string) string {
	return replaceCustomFields(str, fields, func(v string) string { return v })
}

// ReplaceCustomFieldsInURL substitutes the same placeholders as
// ReplaceCustomFields, percent-encoding every value it puts in.
//
// Encoding happens here and not in ReplaceCustomFields because this is the one
// caller that knows the context of the result. In a body the same placeholder
// may be prose, an attribute or a URL, so escaping it would corrupt as often as
// it would help; here the string is a URL by construction. Without it, an
// address like `mario+rossi@example.com` reaches the endpoint as
// `mario rossi@example.com`, and a value containing `&` silently appends a
// parameter to someone else's URL.
//
// Values are escaped for a query component, which is where a placeholder in an
// unsubscribe URL sits in practice. A value substituted into a path segment is
// still safe, but a space in one arrives as `+` rather than `%20`.
func ReplaceCustomFieldsInURL(str string, fields map[string]string) string {
	return replaceCustomFields(str, fields, url.QueryEscape)
}

func replaceCustomFields(str string, fields map[string]string, escape func(string) string) string {
	for key, value := range fields {
		regExp := fmt.Sprintf(`\{\{ *%s *\}\}`, key)
		reg, err := regexp.Compile(regExp)
		if err != nil {
			continue
		}
		str = reg.ReplaceAllString(str, escape(value))
	}
	return str
}

// placeholderReg matches a placeholder in the shape replaceCustomFields
// substitutes: `{{ name }}`, with optional spaces and no nesting.
var placeholderReg = regexp.MustCompile(`\{\{ *[^{}]* *\}\}`)

// HasUnresolvedPlaceholders reports whether a substituted string still contains
// a placeholder — meaning the fields on hand did not name it.
//
// ReplaceCustomFields leaves such a placeholder verbatim rather than blanking
// it. In a body that is cosmetic; in a header that drives an unauthenticated
// POST it is the difference between an unsubscribe that works and one that only
// claims to, so the callers that build headers check for it.
func HasUnresolvedPlaceholders(str string) bool {
	return placeholderReg.MatchString(str)
}

// EffectiveFields is the field map one Delivery is rendered with: the
// Recipient's own fields, plus `email` holding the Recipient's address.
//
// The injected value is a **default**, not an override. A caller that passes an
// `email` field of its own keeps its value, because silently replacing it would
// change what templates already in production render, with nothing to tell the
// caller why.
//
// This is the single definition of "the fields available to this Delivery", and
// it is deliberately shared by the intake check and the renderer: were they to
// disagree, intake would accept a Recipient the Builder cannot resolve, or
// refuse one it could.
func EffectiveFields(email string, fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields)+1)
	out["email"] = email
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// ReplaceLiteralFields substitutes each original placeholder once, treating keys
// and values literally. Replacement values are never interpreted as templates.
func ReplaceLiteralFields(text string, fields map[string]string) string {
	return placeholderReg.ReplaceAllStringFunc(text, func(token string) string {
		if value, ok := fields[strings.TrimSpace(token[2:len(token)-2])]; ok {
			return value
		}
		return token
	})
}

// LiteralFieldsSize is len(ReplaceLiteralFields(text, fields)), computed without
// building the result: each placeholder a field names changes the length by the
// difference between its value and itself.
//
// It exists so a size limit can be enforced before rendering rather than after.
// The amplification is multiplicative — a placeholder repeated a hundred
// thousand times in a body, against a value of fifty kilobytes, renders to five
// gigabytes from a request of well under a megabyte — so measuring the rendered
// string would already have paid for it.
func LiteralFieldsSize(text string, fields map[string]string) int {
	size := len(text)
	for rest := text; ; {
		loc := placeholderReg.FindStringIndex(rest)
		if loc == nil {
			return size
		}
		token := rest[loc[0]:loc[1]]
		if value, ok := fields[strings.TrimSpace(token[2:len(token)-2])]; ok {
			size += len(value) - len(token)
		}
		rest = rest[loc[1]:]
	}
}

// ReplaceCustomFieldsWithin is ReplaceCustomFields refusing, with false, a
// result longer than limit, without ever holding a string much longer than it.
//
// Unlike the literal semantics, the legacy ones cannot be measured without
// rendering: keys are substituted one after another, in map order, so a value
// may hold a placeholder a later key expands, and a value is a regexp template,
// so a `$0` in it expands to the whole placeholder it replaces. Each key's pass
// is therefore measured before it is built, and a pass that would cross the
// limit stops the substitution there.
//
// A `$` makes the exact length of one expansion expensive to know in advance.
// It is bounded by the value's length plus the placeholder's length for each
// `$` in it, since a reference expands to at most the whole match, and only
// expanded when that bound fits within limit. A value whose bound does not is
// refused even if its exact expansion might have fitted: no legitimate value
// needs that many references, and a literal `$` already renders broken here.
func ReplaceCustomFieldsWithin(str string, fields map[string]string, limit int) (string, bool) {
	return replaceCustomFieldsWithin(str, fields, func(v string) string { return v }, limit)
}

// ReplaceCustomFieldsInURLWithin is ReplaceCustomFieldsInURL under the limit of
// ReplaceCustomFieldsWithin.
func ReplaceCustomFieldsInURLWithin(str string, fields map[string]string, limit int) (string, bool) {
	return replaceCustomFieldsWithin(str, fields, url.QueryEscape, limit)
}

func replaceCustomFieldsWithin(str string, fields map[string]string, escape func(string) string, limit int) (string, bool) {
	var scratch []byte
	for key, value := range fields {
		reg, err := regexp.Compile(fmt.Sprintf(`\{\{ *%s *\}\}`, key))
		if err != nil {
			continue
		}
		matches := reg.FindAllStringSubmatchIndex(str, -1)
		if len(matches) == 0 {
			continue
		}
		value = escape(value)
		dollars := strings.Count(value, "$")
		size, expanded := len(str), 0
		for _, m := range matches {
			e := len(value)
			if dollars > 0 {
				if len(value)+dollars*(m[1]-m[0]) > limit {
					return "", false
				}
				scratch = reg.ExpandString(scratch[:0], value, str, m)
				e = len(scratch)
			}
			// The result holds every expansion, so their sum alone crossing the
			// limit settles it, whatever the placeholders they replace give back.
			expanded += e
			size += e - (m[1] - m[0])
			if expanded > limit {
				return "", false
			}
		}
		if size > limit {
			return "", false
		}
		out := make([]byte, 0, size)
		last := 0
		for _, m := range matches {
			out = append(out, str[last:m[0]]...)
			out = reg.ExpandString(out, value, str, m)
			last = m[1]
		}
		str = string(append(out, str[last:]...))
	}
	return str, len(str) <= limit
}
