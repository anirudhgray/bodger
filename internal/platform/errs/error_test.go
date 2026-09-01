package errs_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestNew_SeedsRegistryDefault(t *testing.T) {
	err := errs.New(errs.NotFound)

	if err.Code != errs.NotFound {
		t.Errorf("Code = %q, want %q", err.Code, errs.NotFound)
	}
	if err.Message != errs.DefaultMessage(errs.NotFound) {
		t.Errorf("Message = %q, want the registry default %q", err.Message, errs.DefaultMessage(errs.NotFound))
	}
}

func TestNew_PanicsOnUnregisteredCode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(unregistered code) did not panic")
		}
	}()
	errs.New(errs.Code("not-a-real-code"))
}

func TestBuilder_ChainsAndSetsEveryField(t *testing.T) {
	err := errs.New(errs.NotFound).
		Explain("No account called %q.", "hdcf").
		Field("account_ref").
		With("candidates", []string{"HDFC Savings"})

	if err.Code != errs.NotFound {
		t.Errorf("Code = %q, want not_found", err.Code)
	}
	if want := `No account called "hdcf".`; err.Message != want {
		t.Errorf("Message = %q, want %q", err.Message, want)
	}
	if err.FieldPath != "account_ref" {
		t.Errorf("FieldPath = %q, want account_ref", err.FieldPath)
	}
	candidates, ok := err.Details["candidates"].([]string)
	if !ok || len(candidates) != 1 || candidates[0] != "HDFC Savings" {
		t.Errorf("Details[candidates] = %#v, want [HDFC Savings]", err.Details["candidates"])
	}
}

func TestExplain_LiteralPercentMustBeEscaped(t *testing.T) {
	// Explain is a fmt format string, exactly like fmt.Errorf: a literal
	// "%" needs "%%" even with no other arguments. This is what lets go
	// vet check every call site's verbs against its arguments.
	err := errs.New(errs.InvalidInput).Explain("value must be 100%% numeric")
	if want := "value must be 100% numeric"; err.Message != want {
		t.Errorf("Message = %q, want %q", err.Message, want)
	}
}

func TestError_ImplementsErrorInterface(t *testing.T) {
	var err error = errs.New(errs.Conflict).Explain("An account named %q already exists.", "Savings")
	want := "conflict: An account named \"Savings\" already exists."
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestCLIMessage_OmitsFieldWhenUnset(t *testing.T) {
	err := errs.New(errs.InvalidInput).Explain("Amount must be positive.")
	if got := err.CLIMessage(); got != "Amount must be positive." {
		t.Errorf("CLIMessage() = %q, want %q", got, "Amount must be positive.")
	}
}

// TestCLIMessage_NeverIncludesFieldPath locks in that FieldPath never
// leaks into CLI output, however it's set. FieldPath is an internal
// identifier for the --json field key and a REST surface's form
// attribution, not for a human reading the terminal — see CLIMessage's
// doc comment.
func TestCLIMessage_NeverIncludesFieldPath(t *testing.T) {
	err := errs.New(errs.InvalidInput).Explain("Amount must be positive.").Field("amount")
	want := "Amount must be positive."
	if got := err.CLIMessage(); got != want {
		t.Errorf("CLIMessage() = %q, want %q", got, want)
	}
}

func TestHTTPStatusAndCLIExitCode_MatchRegistryForEveryCode(t *testing.T) {
	for _, row := range shipped {
		err := errs.New(row.code)
		if got := err.HTTPStatus(); got != row.http {
			t.Errorf("%s: (*Error).HTTPStatus() = %d, want %d", row.wire, got, row.http)
		}
		if got := err.CLIExitCode(); got != row.cli {
			t.Errorf("%s: (*Error).CLIExitCode() = %d, want %d", row.wire, got, row.cli)
		}
	}
}

// TestError_CauseNeverReachesSurfaces is issue #4's acceptance test,
// verbatim: construct an error wrapping a synthetic driver failure
// containing a fake SQL fragment and a file path, render it through a JSON
// encoder path and a CLI-style formatter path, and assert neither string
// appears in either output — while the full chain is present in the log
// record.
func TestError_CauseNeverReachesSurfaces(t *testing.T) {
	// No embedded double quotes: the log assertion below checks this
	// fragment as a literal substring of a JSON-encoded log record, where
	// a literal " would be escaped to \" and never match.
	const sqlFragment = `near WHERE: syntax error in SELECT * FROM transactions WHERE account_id = ?`
	const filePath = `/var/lib/bodger/data/bodger.db`

	cause := fmt.Errorf("open %s: %w", filePath, fmt.Errorf("driver: %s", sqlFragment))

	err := errs.New(errs.NotFound).
		Explain("No account called %q.", "hdcf").
		Field("account_ref").
		With("candidates", []string{"HDFC Savings"}).
		Wrap(cause)

	sensitive := []string{sqlFragment, filePath}

	// JSON encoder path — what a REST surface would return in a 422/404
	// body.
	jsonBody, err2 := json.Marshal(err)
	if err2 != nil {
		t.Fatalf("json.Marshal: %v", err2)
	}
	jsonOut := string(jsonBody)

	// CLI-style formatter path — what a CLI surface would print.
	cliOut := err.CLIMessage()

	for _, s := range sensitive {
		if strings.Contains(jsonOut, s) {
			t.Errorf("JSON output leaked cause detail %q\ngot: %s", s, jsonOut)
		}
		if strings.Contains(cliOut, s) {
			t.Errorf("CLI output leaked cause detail %q\ngot: %s", s, cliOut)
		}
	}

	// The safe domain detail must still reach both surfaces.
	for _, out := range []string{jsonOut, cliOut} {
		if !strings.Contains(out, "hdcf") {
			t.Errorf("output is missing the safe domain detail %q: %s", "hdcf", out)
		}
	}

	// The log record path — the full chain must be present here, in full.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Error("account lookup failed", "error", err)
	logOut := buf.String()

	for _, s := range sensitive {
		if !strings.Contains(logOut, s) {
			t.Errorf("log record is missing cause detail %q\ngot: %s", s, logOut)
		}
	}
}

func TestError_MarshalJSON_OmitsEmptyFieldAndDetails(t *testing.T) {
	err := errs.New(errs.Unavailable)

	body, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatalf("json.Marshal: %v", mErr)
	}

	var decoded map[string]any
	if uErr := json.Unmarshal(body, &decoded); uErr != nil {
		t.Fatalf("json.Unmarshal: %v", uErr)
	}
	if _, ok := decoded["field"]; ok {
		t.Errorf("unset FieldPath was serialised: %s", body)
	}
	if _, ok := decoded["details"]; ok {
		t.Errorf("unset Details was serialised: %s", body)
	}
	if decoded["code"] != string(errs.Unavailable) {
		t.Errorf("code = %v, want %q", decoded["code"], errs.Unavailable)
	}
}

func TestError_LogValue_OmitsCauseWhenNotWrapped(t *testing.T) {
	err := errs.New(errs.NotFound).Explain("No account called %q.", "hdcf")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Error("lookup failed", "error", err)

	if strings.Contains(buf.String(), `"cause"`) {
		t.Errorf("log record has a cause field with nothing wrapped: %s", buf.String())
	}
}
