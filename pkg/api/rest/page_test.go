package rest

import (
	"encoding/base64"
	"errors"
	"net/url"
	"testing"
)

func nonEmpty(s string) bool { return s != "" }

// nextCursor pages rows with the given limit and returns the cursor that follows them.
func nextCursor(t *testing.T, limit string, rows []string, scope ...any) string {
	t.Helper()
	p, err := readPage(url.Values{"limit": {limit}}, func(string) bool { return true }, scope...)
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := writePage(p, rows, func(s string) string { return s })
	if err != nil || next == nil {
		t.Fatal(next, err)
	}
	return *next
}

func requireProblem(t *testing.T, err error, code string) {
	t.Helper()
	var p *requestError
	if !errors.As(err, &p) || p.status != 400 || p.code != code {
		t.Fatalf("want 400 %s, got %v", code, err)
	}
}

func TestPageRoundTripsCursorWithinScope(t *testing.T) {
	rows := []string{"a", "b", "c"}
	first, err := readPage(url.Values{"limit": {"2"}}, nonEmpty, "letters", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ks := first.keyset(); ks.After != nil || ks.Limit != 3 {
		t.Fatalf("keyset must start unbounded with one lookahead row: %+v", ks)
	}
	items, next, err := writePage(first, rows, func(s string) string { return s })
	if err != nil || len(items) != 2 || next == nil {
		t.Fatal(items, next, err)
	}

	second, err := readPage(url.Values{"cursor": {*next}}, nonEmpty, "letters", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ks := second.keyset(); ks.After == nil || *ks.After != "b" || ks.Limit != 101 {
		t.Fatalf("cursor must resume after the last returned row with the default limit: %+v", ks)
	}
	items, next, err = writePage(second, rows[2:], func(s string) string { return s })
	if err != nil || len(items) != 1 || next != nil {
		t.Fatal(items, next, err)
	}
}

func TestPageRefusesCursorFromAnotherScope(t *testing.T) {
	next := nextCursor(t, "1", []string{"a", "b"}, "letters", "example.com")
	for _, scope := range [][]any{{"letters", "other.com"}, {"numbers", "example.com"}, {"letters", "example.com", "filtered"}, {"lettersexample.com"}} {
		_, err := readPage(url.Values{"cursor": {next}}, nonEmpty, scope...)
		requireProblem(t, err, "invalid_cursor")
	}
}

func TestPageRefusesMalformedCursor(t *testing.T) {
	scope := []any{"letters"}
	emptyKey := nextCursor(t, "1", []string{"", "x"}, scope...)
	for _, c := range []string{"", "!!!", base64.RawURLEncoding.EncodeToString([]byte("not json")), base64.RawURLEncoding.EncodeToString([]byte(`{"s":1}`)), emptyKey} {
		_, err := readPage(url.Values{"cursor": {c}}, nonEmpty, scope...)
		requireProblem(t, err, "invalid_cursor")
	}
}

func TestPageLimitBounds(t *testing.T) {
	for raw, want := range map[string]int{"1": 1, "1000": 1000} {
		p, err := readPage(url.Values{"limit": {raw}}, nonEmpty, "letters")
		if err != nil || p.limit != want {
			t.Fatal(raw, p.limit, err)
		}
	}
	for _, raw := range []string{"0", "1001", "-1", "x", ""} {
		_, err := readPage(url.Values{"limit": {raw}}, nonEmpty, "letters")
		requireProblem(t, err, "invalid_query_parameter")
	}
}
