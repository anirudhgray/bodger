package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
)

// runWithStdin is run's counterpart for commands that read from stdin
// (set-password): a non-*os.File io.Reader, so readNewPassword takes its
// "piped" branch rather than trying term.IsTerminal on it — exactly what a
// real piped invocation (`echo "pw" | bodger auth set-password`) does.
func runWithStdin(t *testing.T, factory clisurface.ServiceFactory, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "bodger", SilenceUsage: true, SilenceErrors: true}
	clisurface.Register(root, factory)

	var outBuf, errBuf bytes.Buffer
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func mustRunWithStdin(t *testing.T, factory clisurface.ServiceFactory, stdin string, args ...string) string {
	t.Helper()
	stdout, stderr, err := runWithStdin(t, factory, stdin, args...)
	if err != nil {
		t.Fatalf("run(%v): %v (stderr: %s)", args, err, stderr)
	}
	return stdout
}

// TestAuthSetPassword_TakesEffect proves set-password's whole point: the
// password it sets is the one Login later verifies against. The CLI has no
// login command of its own (that's issue #56's REST API surface), so this
// calls internal/app.Login directly against the same database
// set-password just wrote to.
func TestAuthSetPassword_TakesEffect(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	stdout := mustRunWithStdin(t, factory, "correcthorsebattery\n", "auth", "set-password", "--json")

	var got struct {
		Message string `json:"message"`
	}
	decodeData(t, stdout, &got)
	if got.Message == "" {
		t.Error("message is empty, want a confirmation")
	}

	svc, closeDB, err := factory(context.Background())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer func() { _ = closeDB() }()

	result, err := svc.Login(context.Background(), app.LoginCommand{Password: "correcthorsebattery"})
	if err != nil {
		t.Fatalf("Login with the password just set: %v", err)
	}
	if result.ActorID != ports.SeededUserID {
		t.Errorf("ActorID = %q, want %q", result.ActorID, ports.SeededUserID)
	}

	if _, err := svc.Login(context.Background(), app.LoginCommand{Password: "wrong-password"}); err == nil {
		t.Error("Login with the wrong password succeeded, want an error")
	}
}

// TestAuthSetPassword_TooShort proves the CLI surfaces
// internal/app.SetPassword's own minimum-length validation as a normal
// *errs.Error rather than swallowing or reformatting it.
func TestAuthSetPassword_TooShort(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	_, _, err := runWithStdin(t, factory, "short\n", "auth", "set-password")
	wantErrCode(t, err, errs.InvalidInput)
}

// TestAuthToken_CreateListRevoke exercises the full API-token lifecycle
// through the CLI: create shows the plaintext once, list shows it live and
// unrevoked, revoke succeeds, and a second list reflects the revocation.
func TestAuthToken_CreateListRevoke(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	createOut := mustRun(t, factory, "auth", "token", "create", "laptop", "--json")
	var created struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	decodeData(t, createOut, &created)
	if created.Token == "" {
		t.Fatal("created token's plaintext is empty")
	}
	if created.Name != "laptop" {
		t.Errorf("name = %q, want %q", created.Name, "laptop")
	}

	listOut := mustRun(t, factory, "auth", "token", "list", "--json")
	var listed []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Token   string `json:"token,omitempty"`
		Revoked bool   `json:"revoked"`
	}
	decodeData(t, listOut, &listed)
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if listed[0].ID != created.ID {
		t.Errorf("listed id = %q, want %q", listed[0].ID, created.ID)
	}
	if listed[0].Token != "" {
		t.Error("listed token exposes the plaintext; want it shown only at creation")
	}
	if listed[0].Revoked {
		t.Error("freshly created token is Revoked, want not")
	}

	mustRun(t, factory, "auth", "token", "revoke", created.ID)

	listOut = mustRun(t, factory, "auth", "token", "list", "--json")
	decodeData(t, listOut, &listed)
	if !listed[0].Revoked {
		t.Error("revoked token is not Revoked, want it to be")
	}
}

// TestAuthToken_RevokeUnknown proves revoking a nonexistent token surfaces
// the same NotFound *errs.Error every other actor-scoped repository lookup
// in this codebase returns, rather than a bare success or a different code.
func TestAuthToken_RevokeUnknown(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	_, _, err := run(t, factory, "auth", "token", "revoke", "does-not-exist")
	wantErrCode(t, err, errs.NotFound)
}
