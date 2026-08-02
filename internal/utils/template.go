package utils

import (
	"fmt"
	"net/url"
	"regexp"
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
