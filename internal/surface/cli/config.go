// This file implements issue #136's `bodger config reporting-currency`
// surface, wired onto internal/app/reporting_currency.go's use cases
// (issue #132) the same one-command, one-application-call way as every
// other command in this package.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/ports"
)

// newConfigCmd builds the "config" command group: "reporting-currency" for
// now, room for more instance-level settings later.
func newConfigCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and change your bodger configuration",
	}
	cmd.AddCommand(newConfigReportingCurrencyCmd(factory))
	return cmd
}

// newConfigReportingCurrencyCmd builds the "config reporting-currency"
// subgroup: "get" and "set", one per
// internal/app/reporting_currency.go's use cases.
func newConfigReportingCurrencyCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reporting-currency",
		Short: "View or change the currency balances and reports convert into",
	}
	cmd.AddCommand(
		newConfigReportingCurrencyGetCmd(factory),
		newConfigReportingCurrencySetCmd(factory),
	)
	return cmd
}

// reportingCurrencyView renders GetReportingCurrency's result. IsSet
// distinguishes "never configured" from a currency that happens to render
// the same either way, so --json always tells a caller which case it got
// without parsing the human message.
type reportingCurrencyView struct {
	Currency string `json:"currency"`
	IsSet    bool   `json:"is_set"`
}

// printReportingCurrency renders v. instanceDefault is the instance's own
// fallback currency (internal/platform/config.Config.DefaultCurrency,
// ADR-0004's bottom currency-precedence rung) — reported here rather than
// generically, since --currency get is precisely the command a user runs
// to find out what they're actually falling back to.
func printReportingCurrency(w io.Writer, v reportingCurrencyView, instanceDefault string) {
	if !v.IsSet {
		_, _ = fmt.Fprintf(w, "Not set — falls back to this instance's default (%s).\n", instanceDefault)
		return
	}
	_, _ = fmt.Fprintln(w, v.Currency)
}

func newConfigReportingCurrencyGetCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "See your configured reporting currency",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			currency, err := svc.GetReportingCurrency(ctx, ports.SeededUserID)
			if err != nil {
				return err
			}
			view := reportingCurrencyView{Currency: currency, IsSet: currency != ""}
			return render(cmd, view, func(w io.Writer) { printReportingCurrency(w, view, svc.Config.DefaultCurrency) })
		},
	}
}

func newConfigReportingCurrencySetCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "set <currency>",
		Short: "Set your reporting currency",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			if err := svc.SetReportingCurrency(ctx, ports.SeededUserID, args[0]); err != nil {
				return err
			}
			view := statusView{Message: fmt.Sprintf("Reporting currency set to %s.", args[0])}
			return render(cmd, view, func(w io.Writer) { printStatus(w, view) })
		},
	}
}
