// Package config resolves bodger's layered configuration: compiled-in
// instance defaults, overridden by the operator's environment. This is the
// only package in the repository permitted to call os.Getenv (ADR-0005).
//
// bodger is single-user and self-hosted (docs/decisions/0011-error-model.md
// §Context): there is no per-user settings table yet (authentication and
// multi-user are M2, ADR-0006), so for now "the user's" defaults — like
// their reporting timezone — are set the same way instance-wide defaults
// are, through the operator's environment. The two-layer shape (instance
// defaults, then an override layer) is what lets that change later without
// reshaping how the rest of the application reads config.
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"
)

// Config is bodger's resolved configuration.
type Config struct {
	// DefaultCurrency is the ISO 4217 code applied at the bottom of the
	// currency precedence ladder (ADR-0004: entry -> account -> user ->
	// instance) when nothing more specific resolves one.
	DefaultCurrency string
	// UserTimezone is the IANA time zone name used to resolve "today" and
	// reporting-period boundaries (ADR-0005; data-model.md §9). It is
	// never the server's timezone — the process runs under TZ=UTC
	// specifically so a bug that reads the host zone instead of this one
	// fails immediately.
	UserTimezone string
	// DBPath is the filesystem path to bodger's SQLite database file,
	// opened by internal/adapters/sqlite.Open. Relative paths resolve
	// against the process's working directory.
	DBPath string
}

// Defaults are the values bodger ships with when the operator sets no
// override. They are deliberately neutral: UTC and USD assume nothing
// about who is running the instance. DBPath defaults to a file in the
// working directory, which is enough for local and single-container use;
// operators who want it elsewhere set EnvDBPath.
var Defaults = Config{
	DefaultCurrency: "USD",
	UserTimezone:    "UTC",
	DBPath:          "bodger.db",
}

const (
	// EnvDefaultCurrency, when set, overrides Defaults.DefaultCurrency.
	EnvDefaultCurrency = "BODGER_DEFAULT_CURRENCY"
	// EnvUserTimezone, when set, overrides Defaults.UserTimezone.
	EnvUserTimezone = "BODGER_USER_TIMEZONE"
	// EnvDBPath, when set, overrides Defaults.DBPath.
	EnvDBPath = "BODGER_DB_PATH"
)

// currencyPattern is a structural check only — three uppercase ASCII
// letters, the shape every ISO 4217 code has. It deliberately does not
// check the code is a real, known currency: that table belongs to the
// domain layer (ADR-0004), which this platform package does not depend on.
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Load resolves Config by layering the process environment over Defaults:
// a non-empty environment variable wins, otherwise the instance default
// applies. It returns an error if an override is present but structurally
// invalid — an unparseable IANA timezone, or a currency code that isn't
// three uppercase letters — so a misconfigured instance fails at startup
// rather than at the first request that needs the value.
func Load() (Config, error) {
	return load(lookupFunc(os.LookupEnv))
}

// lookupFunc abstracts os.LookupEnv so precedence can be table-tested
// without mutating the real process environment.
type lookupFunc func(key string) (string, bool)

func load(lookup lookupFunc) (Config, error) {
	cfg := Defaults

	if v, ok := lookup(EnvDefaultCurrency); ok && v != "" {
		cfg.DefaultCurrency = v
	}
	if v, ok := lookup(EnvUserTimezone); ok && v != "" {
		cfg.UserTimezone = v
	}
	if v, ok := lookup(EnvDBPath); ok && v != "" {
		cfg.DBPath = v
	}

	if !currencyPattern.MatchString(cfg.DefaultCurrency) {
		return Config{}, fmt.Errorf("config: %s=%q is not a three-letter currency code", EnvDefaultCurrency, cfg.DefaultCurrency)
	}
	if _, err := time.LoadLocation(cfg.UserTimezone); err != nil {
		return Config{}, fmt.Errorf("config: %s=%q is not a known IANA time zone: %w", EnvUserTimezone, cfg.UserTimezone, err)
	}

	return cfg, nil
}
