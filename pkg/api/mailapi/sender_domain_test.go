package mailapi

import "testing"

func TestSenderDomainAllowed(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		tenant string
		want   bool
	}{
		{"exact match", "example.com", "example.com", true},
		{"case insensitive", "Example.COM", "example.com", true},
		{"trailing dot tolerated", "example.com.", "example.com", true},
		{"parent allowed", "example.com", "k.example.com", true},
		{"grandparent allowed", "example.com", "a.b.example.com", true},
		{"child of tenant rejected", "sub.example.com", "example.com", false},
		{"sibling rejected", "evil.com", "example.com", false},
		{"lookalike suffix rejected", "ample.com", "example.com", false},
		{"prefix substring rejected", "example.co", "example.com", false},
		{"empty from rejected", "", "example.com", false},
		{"empty tenant rejected", "example.com", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := senderDomainAllowed(tc.from, tc.tenant)
			if got != tc.want {
				t.Fatalf("senderDomainAllowed(%q, %q) = %v, want %v", tc.from, tc.tenant, got, tc.want)
			}
		})
	}
}
