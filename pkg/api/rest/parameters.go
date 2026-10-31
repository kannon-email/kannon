package rest

import (
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
)

func parseDomainPath(r *http.Request) (values.DomainName, error) {
	d, err := values.Parse(r.PathValue(domainParameter))
	if err != nil {
		return d, problemError(400, "malformed_request")
	}
	return d, nil
}

// Check names deterministically before scalar multiplicity. Only event type repeats.
func parseQuery(raw string, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, problemError(400, "invalid_query_parameter")
	}
	names := make([]string, 0, len(q))
	for name := range q {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !slices.Contains(allowed, name) {
			return nil, problemError(400, "unknown_query_parameter")
		}
	}
	for _, name := range names {
		if name != typeParameter && len(q[name]) != 1 {
			return nil, problemError(400, "invalid_query_parameter")
		}
	}
	return q, nil
}

func pageLimit(q url.Values) (int, error) {
	if !q.Has(limitParameter) {
		return 100, nil
	}
	n, err := strconv.Atoi(q.Get(limitParameter))
	if err != nil || n < 1 || n > 1000 {
		return 0, problemError(400, "invalid_query_parameter")
	}
	return n, nil
}

func timeRange(q url.Values) (stats.TimeRange, error) {
	from, e1 := time.Parse(time.RFC3339Nano, q.Get(fromParameter))
	to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
	if e1 != nil || e2 != nil {
		return stats.TimeRange{}, problemError(400, "invalid_query_parameter")
	}
	return stats.TimeRange{Start: from, Stop: to}, nil
}

const (
	fromParameter    = "from"
	batchIDParameter = "batch_id"
	domainParameter  = "domain"
)

const (
	groupByParameter = "group_by"
	typeParameter    = "type"
)
