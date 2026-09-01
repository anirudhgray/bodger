package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// accountView is this package's JSON- and text-renderable shape for an
// account. Every field is a plain string, int, or bool — this package
// never names internal/domain/ledger.Account directly (see cli.go's
// package doc), even though every value here is read straight off one via
// accountViewFrom.
type accountView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	Currency           string `json:"currency"`
	OpeningBalance     string `json:"opening_balance"`
	OpeningBalanceDate string `json:"opening_balance_date,omitempty"`
	Institution        string `json:"institution,omitempty"`
	SortOrder          int    `json:"sort_order"`
	Archived           bool   `json:"archived"`
	ArchivedAt         string `json:"archived_at,omitempty"`
}

func accountViewFrom(r app.AccountResult) accountView {
	v := accountView{
		ID:             r.Account.ID(),
		Name:           r.Account.Name(),
		Type:           string(r.Account.Kind()),
		Currency:       r.Account.Currency(),
		OpeningBalance: r.Account.OpeningBalance().AmountString(),
		SortOrder:      r.Account.SortOrder(),
		Archived:       r.Account.Archived(),
	}
	if d, ok := r.Account.OpeningBalanceDate(); ok {
		v.OpeningBalanceDate = d.String()
	}
	if inst, ok := r.Account.Institution(); ok {
		v.Institution = inst
	}
	if d, ok := r.Account.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	return v
}

func printAccount(w io.Writer, v accountView) {
	_, _ = fmt.Fprintf(w, "%s (%s, %s) — opening balance %s %s\n", v.Name, v.Type, v.Currency, v.OpeningBalance, v.Currency)
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
}

func printAccountTable(w io.Writer, views []accountView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No accounts yet. Add one with `bodger accounts add`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tTYPE\tCURRENCY\tOPENING BALANCE\tARCHIVED")
	for _, v := range views {
		archived := "no"
		if v.Archived {
			archived = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.Name, v.Type, v.Currency, v.OpeningBalance, archived)
	}
	_ = tw.Flush()
}

// newAccountsCmd builds the "accounts" command group: list, add, archive,
// and set-opening-balance, matching issue #7's scope exactly. RenameAccount
// exists at the application layer (internal/app/accounts.go) but isn't
// wired to a command here — the issue's own worked command list doesn't
// include an accounts rename subcommand, so exposing one is left to a
// future issue rather than assumed.
func newAccountsCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "Manage your accounts",
	}
	cmd.AddCommand(
		newAccountsListCmd(factory),
		newAccountsAddCmd(factory),
		newAccountsArchiveCmd(factory),
		newAccountsSetOpeningBalanceCmd(factory),
	)
	return cmd
}

func newAccountsListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListAccounts(ctx, app.ListAccountsQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]accountView, len(result.Accounts))
			for i, a := range result.Accounts {
				views[i] = accountViewFrom(app.AccountResult{Account: a})
			}
			return render(cmd, views, func(w io.Writer) { printAccountTable(w, views) })
		},
	}
}

type accountAddFlags struct {
	accountType        string
	currency           string
	openingBalance     string
	openingBalanceDate string
	institution        string
	sortOrder          int
}

func newAccountsAddCmd(factory ServiceFactory) *cobra.Command {
	var f accountAddFlags
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
				ActorID:            ports.SeededUserID,
				Name:               args[0],
				Kind:               f.accountType,
				Currency:           f.currency,
				OpeningBalance:     f.openingBalance,
				OpeningBalanceDate: f.openingBalanceDate,
				Institution:        f.institution,
				SortOrder:          f.sortOrder,
			})
			if err != nil {
				return err
			}
			view := accountViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printAccount(w, view) })
		},
	}
	cmd.Flags().StringVar(&f.accountType, "type", "", "account type: bank, cash, credit_card, wallet, investment, loan, or other (required)")
	cmd.Flags().StringVar(&f.currency, "currency", "", "the account's currency code (defaults to your instance default)")
	cmd.Flags().StringVar(&f.openingBalance, "opening-balance", "", "the balance this account started with (defaults to zero)")
	cmd.Flags().StringVar(&f.openingBalanceDate, "opening-balance-date", "", "the date the opening balance is true as of")
	cmd.Flags().StringVar(&f.institution, "institution", "", "the bank or provider this account is held at")
	cmd.Flags().IntVar(&f.sortOrder, "sort-order", 0, "where this account sorts in lists")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newAccountsArchiveCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <account>",
		Short: "Archive an account, hiding it from pickers while keeping its history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ArchiveAccount(ctx, app.ArchiveAccountCommand{
				ActorID:    ports.SeededUserID,
				AccountRef: args[0],
			})
			if err != nil {
				return err
			}
			view := accountViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Archived account %q.\n", view.Name)
			})
		},
	}
}

func newAccountsSetOpeningBalanceCmd(factory ServiceFactory) *cobra.Command {
	var on string
	cmd := &cobra.Command{
		Use:   "set-opening-balance <account> <amount>",
		Short: "Re-declare an account's starting balance",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.SetOpeningBalance(ctx, app.SetOpeningBalanceCommand{
				ActorID:            ports.SeededUserID,
				AccountRef:         args[0],
				OpeningBalance:     args[1],
				OpeningBalanceDate: on,
			})
			if err != nil {
				return err
			}
			view := accountViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Set %q's opening balance to %s %s.\n", view.Name, view.OpeningBalance, view.Currency)
			})
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "the date this balance is true as of (defaults to none declared)")
	return cmd
}
