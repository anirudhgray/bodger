package config

// Precedence is tested against the unexported load(lookupFunc) so it never
// touches the real process environment (os.Setenv would make this test
// order-dependent and unsafe to run with -parallel). That also means this
// test lives in package config, not config_test — the only reason to reach
// past the exported Load().

import (
	"strings"
	"testing"
)

// fakeEnv builds a lookupFunc backed by a plain map, so each test case
// states exactly which variables are "set" without depending on anything
// in the real environment.
func fakeEnv(vars map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestLoad_Precedence(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "no overrides falls back to instance defaults",
			env:  map[string]string{},
			want: Defaults,
		},
		{
			name: "currency override wins over instance default",
			env:  map[string]string{EnvDefaultCurrency: "INR"},
			want: Config{DefaultCurrency: "INR", UserTimezone: Defaults.UserTimezone, DBPath: Defaults.DBPath},
		},
		{
			name: "timezone override wins over instance default",
			env:  map[string]string{EnvUserTimezone: "Asia/Kolkata"},
			want: Config{DefaultCurrency: Defaults.DefaultCurrency, UserTimezone: "Asia/Kolkata", DBPath: Defaults.DBPath},
		},
		{
			name: "db path override wins over instance default",
			env:  map[string]string{EnvDBPath: "/var/lib/bodger/bodger.db"},
			want: Config{DefaultCurrency: Defaults.DefaultCurrency, UserTimezone: Defaults.UserTimezone, DBPath: "/var/lib/bodger/bodger.db"},
		},
		{
			name: "all overrides apply independently",
			env: map[string]string{
				EnvDefaultCurrency: "GBP",
				EnvUserTimezone:    "Europe/London",
				EnvDBPath:          "/data/bodger.db",
			},
			want: Config{DefaultCurrency: "GBP", UserTimezone: "Europe/London", DBPath: "/data/bodger.db"},
		},
		{
			name: "an empty override does not clobber the instance default",
			env: map[string]string{
				EnvDefaultCurrency: "",
				EnvUserTimezone:    "",
				EnvDBPath:          "",
			},
			want: Defaults,
		},
		{
			name:    "structurally invalid currency override is rejected",
			env:     map[string]string{EnvDefaultCurrency: "not-a-code"},
			wantErr: "not a three-letter currency code",
		},
		{
			name:    "unknown IANA timezone override is rejected",
			env:     map[string]string{EnvUserTimezone: "Nowhere/Imaginary"},
			wantErr: "not a known IANA time zone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := load(fakeEnv(tt.env))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("load() returned no error, want one containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("load() error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("load() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoad_ReadsRealEnvironment(t *testing.T) {
	t.Setenv(EnvDefaultCurrency, "JPY")
	t.Setenv(EnvUserTimezone, "Asia/Tokyo")
	t.Setenv(EnvDBPath, "/tmp/bodger-test.db")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	want := Config{DefaultCurrency: "JPY", UserTimezone: "Asia/Tokyo", DBPath: "/tmp/bodger-test.db"}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}
