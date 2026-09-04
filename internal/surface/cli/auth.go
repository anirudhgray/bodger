// This file implements issue #57's CLI auth surface: `bodger auth
// set-password` and `bodger auth token create/list/revoke`, wired onto
// internal/app/auth.go's use cases (issue #55) the same one-command,
// one-application-call way as every other command in this package.
//
// set-password deliberately has no HTTP counterpart (ADR-0006, issue #57's
// "important boundary"): the in-process CLI opens the SQLite file directly
// and needs no credential, so filesystem permissions on bodger.db are the
// actual boundary. A user who forgets their password runs this on the
// machine hosting their database — that only works because this command
// exists nowhere else.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// statusView is this package's shape for a command whose application-layer
// call returns no data, only success or failure (set-password and token
// revoke) — --json still gets a stable envelope rather than an empty one.
type statusView struct {
	Message string `json:"message"`
}

func printStatus(w io.Writer, v statusView) {
	_, _ = fmt.Fprintln(w, v.Message)
}

// newAuthCmd builds the "auth" command group: set-password and the "token"
// subgroup, one subcommand per use case internal/app/auth.go exposes
// (excluding Login/Logout/AuthenticateSession/AuthenticateAPIToken, which
// are the web/API surface's concern, not the local CLI's — see issue #57's
// scope).
func newAuthCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage your password and API tokens",
	}
	cmd.AddCommand(
		newAuthSetPasswordCmd(factory),
		newAuthTokenCmd(factory),
	)
	return cmd
}

func newAuthSetPasswordCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "set-password",
		Short: "Set or change your password",
		Long: "Set or change your password.\n\n" +
			"Prompts for a new password with typed input hidden. This is also how you recover from a " +
			"forgotten password: run it on the machine hosting your database — no existing password is needed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			password, err := readNewPassword(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			if err := svc.SetPassword(ctx, app.SetPasswordCommand{
				ActorID:     ports.SeededUserID,
				NewPassword: password,
			}); err != nil {
				return err
			}
			view := statusView{Message: "Password updated."}
			return render(cmd, view, func(w io.Writer) { printStatus(w, view) })
		},
	}
}

// readNewPassword reads the password set-password will use: hidden,
// confirmed twice, when cmd's stdin is an interactive terminal, or a
// single line otherwise — the same shape scripts and tests pipe a value
// through (there is no second interactive entry to confirm against when
// piped, so none is asked for).
func readNewPassword(cmd *cobra.Command) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return readPasswordInteractive(cmd, f)
	}
	return readPasswordLine(cmd.InOrStdin())
}

func readPasswordInteractive(cmd *cobra.Command, f *os.File) (string, error) {
	errW := cmd.ErrOrStderr()

	_, _ = fmt.Fprint(errW, "New password: ")
	first, err := term.ReadPassword(int(f.Fd()))
	_, _ = fmt.Fprintln(errW)
	if err != nil {
		return "", errs.New(errs.Internal).Explain("Couldn't read the password from the terminal.").Wrap(err)
	}

	_, _ = fmt.Fprint(errW, "Confirm password: ")
	second, err := term.ReadPassword(int(f.Fd()))
	_, _ = fmt.Fprintln(errW)
	if err != nil {
		return "", errs.New(errs.Internal).Explain("Couldn't read the password from the terminal.").Wrap(err)
	}

	if string(first) != string(second) {
		return "", errs.New(errs.InvalidInput).Explain("Passwords didn't match.").Field("new_password")
	}
	return string(first), nil
}

func readPasswordLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", errs.New(errs.Internal).Explain("Couldn't read the password from stdin.").Wrap(err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// newAuthTokenCmd builds the "auth token" command group: create, list, and
// revoke, one per internal/app/auth.go's API-token use cases.
func newAuthTokenCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens",
	}
	cmd.AddCommand(
		newAuthTokenCreateCmd(factory),
		newAuthTokenListCmd(factory),
		newAuthTokenRevokeCmd(factory),
	)
	return cmd
}

// apiTokenView is this package's JSON- and text-renderable shape for an
// API token. Token carries the plaintext value and is only ever populated
// by "token create" — every other view of a token shows Name and metadata
// only, per internal/ports.APIToken's own doc comment ("the plaintext is
// shown exactly once, at creation, and never again").
type apiTokenView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Revoked    bool   `json:"revoked"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	Token      string `json:"token,omitempty"`
}

func apiTokenViewFrom(t ports.APIToken) apiTokenView {
	v := apiTokenView{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt.Format(timeFormat),
		Revoked:   t.RevokedAt != nil,
	}
	if t.LastUsedAt != nil {
		v.LastUsedAt = t.LastUsedAt.Format(timeFormat)
	}
	if t.ExpiresAt != nil {
		v.ExpiresAt = t.ExpiresAt.Format(timeFormat)
	}
	if t.RevokedAt != nil {
		v.RevokedAt = t.RevokedAt.Format(timeFormat)
	}
	return v
}

// timeFormat is this file's rendering of a token timestamp: date and
// minute, in UTC, matching the precision ADR-0006's bookkeeping actually
// stores (sub-minute precision isn't meaningful to a person reading this
// list).
const timeFormat = "2006-01-02 15:04 UTC"

func printAPIToken(w io.Writer, v apiTokenView) {
	_, _ = fmt.Fprintf(w, "Created API token %q.\n", v.Name)
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
	_, _ = fmt.Fprintf(w, "\n%s\n\n", v.Token)
	_, _ = fmt.Fprintln(w, "This is the only time the token is shown. Store it now — bodger keeps only its hash.")
}

func printAPITokenTable(w io.Writer, views []apiTokenView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No API tokens yet. Create one with `bodger auth token create <name>`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tID\tCREATED\tLAST USED\tEXPIRES\tREVOKED")
	for _, v := range views {
		lastUsed := v.LastUsedAt
		if lastUsed == "" {
			lastUsed = "never"
		}
		expires := v.ExpiresAt
		if expires == "" {
			expires = "never"
		}
		revoked := "no"
		if v.Revoked {
			revoked = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, v.ID, v.CreatedAt, lastUsed, expires, revoked)
	}
	_ = tw.Flush()
}

func newAuthTokenCreateCmd(factory ServiceFactory) *cobra.Command {
	var expiresAt string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new API token",
		Long: "Create a new API token.\n\n" +
			"The plaintext value is shown once, in this command's output, and never again — copy it " +
			"somewhere safe before closing the terminal.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{
				ActorID:   ports.SeededUserID,
				Name:      args[0],
				ExpiresAt: expiresAt,
			})
			if err != nil {
				return err
			}
			view := apiTokenViewFrom(result.Token)
			view.Token = result.PlaintextToken
			return render(cmd, view, func(w io.Writer) { printAPIToken(w, view) })
		},
	}
	cmd.Flags().StringVar(&expiresAt, "expires", "", "the date this token stops working (defaults to never)")
	return cmd
}

func newAuthTokenListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your API tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListAPITokens(ctx, app.ListAPITokensQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]apiTokenView, len(result.Tokens))
			for i, t := range result.Tokens {
				views[i] = apiTokenViewFrom(t)
			}
			return render(cmd, views, func(w io.Writer) { printAPITokenTable(w, views) })
		},
	}
}

func newAuthTokenRevokeCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			if err := svc.RevokeAPIToken(ctx, app.RevokeAPITokenCommand{
				ActorID: ports.SeededUserID,
				TokenID: args[0],
			}); err != nil {
				return err
			}
			view := statusView{Message: "Revoked API token."}
			return render(cmd, view, func(w io.Writer) { printStatus(w, view) })
		},
	}
}
