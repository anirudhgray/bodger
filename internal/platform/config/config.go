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
	"net"
	"os"
	"regexp"
	"strings"
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
	// HTTPBindAddr is the "host:port" address `bodger serve` (issue #8)
	// listens on. This package validates it's structurally a valid
	// host:port address at load time (see validateHTTPBindAddr), so
	// there is no code path that can start a server with an unparseable
	// bind address. Whether a *non-loopback* bind is actually safe
	// depends on whether authentication has been configured — database
	// state this package has no access to — so that half of ADR-0006's
	// rule is enforced by internal/surface/http at startup instead (see
	// IsLoopback and internal/app.Service.IsAuthConfigured).
	HTTPBindAddr string
	// LogLevel is the minimum level internal/platform/logging.New's
	// *slog.Logger emits records at: "debug", "info", "warn", or "error"
	// (case-insensitive). cmd/bodger constructs exactly one logger, shared
	// by the CLI and `bodger serve`, from this value (issue #43) — both
	// default to "info" rather than picking different levels, since today
	// the only thing either surface actually logs through it is an
	// *errs.Error's cause chain at Error level (ADR-0011), which a "warn"
	// or "debug" default wouldn't change; revisit if either surface grows
	// its own Info/Debug logging that should differ by default.
	LogLevel string
	// FxProviderBaseURL is the base URL of the FX rate provider FetchFxRates
	// (issue #135) fetches from -- ADR-0012's `fx.provider_base_url`.
	// Empty (the default) means the provider adapter's own default
	// (internal/adapters/fxprovider.DefaultBaseURL, the public Frankfurter
	// instance); a self-hoster running their own instance sets this
	// instead of a code change.
	FxProviderBaseURL string
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
	HTTPBindAddr:    "127.0.0.1:8080",
	LogLevel:        "info",
}

const (
	// EnvDefaultCurrency, when set, overrides Defaults.DefaultCurrency.
	EnvDefaultCurrency = "BODGER_DEFAULT_CURRENCY"
	// EnvUserTimezone, when set, overrides Defaults.UserTimezone.
	EnvUserTimezone = "BODGER_USER_TIMEZONE"
	// EnvDBPath, when set, overrides Defaults.DBPath.
	EnvDBPath = "BODGER_DB_PATH"
	// EnvHTTPBindAddr, when set, overrides Defaults.HTTPBindAddr.
	EnvHTTPBindAddr = "BODGER_HTTP_BIND_ADDR"
	// EnvLogLevel, when set, overrides Defaults.LogLevel.
	EnvLogLevel = "BODGER_LOG_LEVEL"
	// EnvFxProviderBaseURL, when set, overrides Defaults.FxProviderBaseURL.
	EnvFxProviderBaseURL = "BODGER_FX_PROVIDER_BASE_URL"
)

// currencyPattern is a structural check only — three uppercase ASCII
// letters, the shape every ISO 4217 code has. It deliberately does not
// check the code is a real, known currency: that table belongs to the
// domain layer (ADR-0004), which this platform package does not depend on.
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// logLevels is the closed set of level names LogLevel accepts,
// case-insensitively. Kept as this package's own list rather than
// importing internal/platform/logging to check against it — the same
// reason currencyPattern doesn't import the domain currency table: this
// package validates structure only, and internal/platform/logging.ParseLevel
// is what actually turns a validated value into a slog.Level.
var logLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

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
	if v, ok := lookup(EnvHTTPBindAddr); ok && v != "" {
		cfg.HTTPBindAddr = v
	}
	if v, ok := lookup(EnvLogLevel); ok && v != "" {
		cfg.LogLevel = v
	}
	if v, ok := lookup(EnvFxProviderBaseURL); ok && v != "" {
		cfg.FxProviderBaseURL = v
	}

	if !currencyPattern.MatchString(cfg.DefaultCurrency) {
		return Config{}, fmt.Errorf("config: %s=%q is not a three-letter currency code", EnvDefaultCurrency, cfg.DefaultCurrency)
	}
	if _, err := time.LoadLocation(cfg.UserTimezone); err != nil {
		return Config{}, fmt.Errorf("config: %s=%q is not a known IANA time zone: %w", EnvUserTimezone, cfg.UserTimezone, err)
	}
	if err := validateHTTPBindAddr(cfg.HTTPBindAddr); err != nil {
		return Config{}, err
	}
	if !logLevels[strings.ToLower(cfg.LogLevel)] {
		return Config{}, fmt.Errorf("config: %s=%q is not a recognised log level (want debug, info, warn, or error)", EnvLogLevel, cfg.LogLevel)
	}

	return cfg, nil
}

// validateHTTPBindAddr checks that addr is structurally a valid
// "host:port" address. It used to also reject a non-loopback host
// outright (ADR-0006's original rule), but that decision needs to know
// whether authentication has been configured — database state this
// package has no access to (it only reads the environment, ADR-0005) —
// so the non-loopback half of the rule now lives in
// internal/surface/http's startup check instead, which has a *Service to
// ask. See IsLoopback.
func validateHTTPBindAddr(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("config: %s=%q is not a valid host:port address: %w", EnvHTTPBindAddr, addr, err)
	}
	return nil
}

// IsLoopback reports whether addr's host is loopback: "localhost", or any
// IP net.ParseIP recognises as loopback (127.0.0.0/8, ::1). An empty host
// (":8080") binds every interface and is not loopback. This is the
// boundary internal/surface/http's startup check (ADR-0006) uses to
// decide whether a bind requires authentication to already be configured
// — see that package's server.go.
func IsLoopback(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("config: %q is not a valid host:port address: %w", addr, err)
	}
	return isLoopbackHost(host), nil
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
