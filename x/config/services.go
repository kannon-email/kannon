package config

import (
	"fmt"

	"github.com/kannon-email/kannon/x/config/envref"
	"github.com/spf13/viper"
)

// servicesKey is the section that decides what a Kannon process is.
const servicesKey = "services"

// Service is one runnable's switch.
type Service struct {
	// Enabled starts this runnable in this process. False by default: a Kannon
	// binary is every component at once, and which of them a given pod runs is
	// the deployment's decision, not a default anybody should inherit.
	Enabled bool `mapstructure:"enabled"`
}

// Services says which of Kannon's runnables a process starts.
//
// It is a section of the configuration file rather than a set of flags so that
// one file can describe a whole installation — every Deployment mounting the same
// ConfigMap and differing only in the variables its own `enabled` references
// name:
//
//	services:
//	  api:
//	    enabled: env://KANNON_ENABLE_API:-false
//	  stats:
//	    enabled: env://KANNON_ENABLE_STATS:-false
//
// Note that services.audit is the audit *writer*, and is not the same switch as
// audit.enabled, which governs whether authorization decisions are published at
// all. Both are still needed to collect an audit trail (ADR 0010): the producer
// runs inside the API process, the writer is a runnable of its own.
type Services struct {
	Sender     Service `mapstructure:"sender"`
	Dispatcher Service `mapstructure:"dispatcher"`
	Validator  Service `mapstructure:"validator"`
	Stats      Service `mapstructure:"stats"`
	Tracker    Service `mapstructure:"tracker"`
	API        Service `mapstructure:"api"`
	SMTP       Service `mapstructure:"smtp"`
	Audit      Service `mapstructure:"audit"`
}

// LoadServices resolves which runnables this process starts, from the `services`
// section and from the deprecated --run-* flags.
//
// The two are OR-ed rather than ranked: a flag on the command line and a key in
// the file are both an operator asking for that component, and nobody passes
// --run-stats meaning "not stats". It also keeps the migration free of order —
// a deployment can move one Deployment at a time to the file while the rest still
// pass flags, with no combination that turns something off unexpectedly.
//
// An error, not a panic like LoadSection: this section decides whether the
// process does anything at all, so getting it wrong deserves a message.
func LoadServices() (Services, error) {
	var s Services
	// errorOnUnknownKeys, so `services: {stat: {enabled: true}}` is refused
	// instead of quietly producing a process that starts nothing.
	if err := viper.UnmarshalKey(servicesKey, &s, envref.Decoder(), errorOnUnknownKeys); err != nil {
		return Services{}, fmt.Errorf("cannot read the %q section: %w", servicesKey, err)
	}

	for _, svc := range s.each() {
		if viper.GetBool(deprecatedRunKey(svc.name)) {
			*svc.enabled = true
		}
	}

	return s, nil
}

// AllServices enables every runnable, which is what `kannon standalone` is.
func AllServices() Services {
	var s Services
	for _, svc := range s.each() {
		*svc.enabled = true
	}
	return s
}

// serviceRef pairs a service's name with the field holding its switch, so the
// name in the file, the name of its deprecated flag and the name in a log line
// cannot come to be spelled three ways.
type serviceRef struct {
	name    string
	enabled *bool
}

func (s *Services) each() []serviceRef {
	return []serviceRef{
		{name: "sender", enabled: &s.Sender.Enabled},
		{name: "dispatcher", enabled: &s.Dispatcher.Enabled},
		{name: "validator", enabled: &s.Validator.Enabled},
		{name: "stats", enabled: &s.Stats.Enabled},
		{name: "tracker", enabled: &s.Tracker.Enabled},
		{name: "api", enabled: &s.API.Enabled},
		{name: "smtp", enabled: &s.SMTP.Enabled},
		{name: "audit", enabled: &s.Audit.Enabled},
	}
}

// deprecatedRunKey is the viper key of the flag that used to be the only way to
// select a runnable: --run-stats for services.stats, and so on for each.
func deprecatedRunKey(service string) string {
	return "run-" + service
}
