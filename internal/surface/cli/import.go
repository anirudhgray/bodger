package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/ports"
)

// importBatchView is this package's JSON- and text-renderable shape for
// an import — ADR-0008's staged pipeline's own unit of work: one uploaded
// file, staged and then reviewed, committed, or rolled back as a whole.
// "batch" names the Go type here (matching internal/app's own command
// names) but never appears in a string a person reads — see cli.go's
// package doc and docs/ux-principles.md §2 — since "import" alone is what
// a user calls the same thing.
type importBatchView struct {
	ID              string `json:"id"`
	SourceFormat    string `json:"source_format"`
	Filename        string `json:"filename"`
	FileHash        string `json:"file_hash"`
	TargetAccountID string `json:"target_account_id"`
	Status          string `json:"status"`
}

func importBatchViewFrom(r app.GetImportBatchResult) importBatchView {
	b := r.Batch
	return importBatchView{
		ID:              b.ID(),
		SourceFormat:    b.SourceFormat(),
		Filename:        b.Filename(),
		FileHash:        b.FileHash(),
		TargetAccountID: b.TargetAccountID(),
		Status:          string(b.Status()),
	}
}

func printImportBatch(w io.Writer, v importBatchView) {
	_, _ = fmt.Fprintf(w, "Import %s: %s (%s)\n", v.ID, v.Filename, v.Status)
	_, _ = fmt.Fprintf(w, "account: %s\n", v.TargetAccountID)
}

func printImportBatchTable(w io.Writer, views []importBatchView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No imports yet. Start one with `bodger import upload <file>`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tFILENAME\tFORMAT\tSTATUS")
	for _, v := range views {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.ID, v.Filename, v.SourceFormat, v.Status)
	}
	_ = tw.Flush()
}

// duplicateMatchView is this package's shape for a candidate duplicate
// ADR-0008's two-tier detection found for a staged record.
type duplicateMatchView struct {
	Tier                 string `json:"tier"`
	MatchedTransactionID string `json:"matched_transaction_id"`
	Resolution           string `json:"resolution"`
}

// occurrenceMatchView is this package's shape for a candidate occurrence
// match issue #301's detection found for a staged record — mirroring
// duplicateMatchView's own field-for-field shape.
type occurrenceMatchView struct {
	OccurrenceID string `json:"occurrence_id"`
	Resolution   string `json:"resolution"`
}

// importRecordView is this package's shape for one staged row.
type importRecordView struct {
	ID                        string               `json:"id"`
	ImportBatchID             string               `json:"import_id"`
	BookedDate                string               `json:"booked_date"`
	PostedDate                string               `json:"posted_date,omitempty"`
	Description               string               `json:"description"`
	Amount                    string               `json:"amount"`
	Currency                  string               `json:"currency"`
	ExternalID                string               `json:"external_id,omitempty"`
	ResolvedAccountID         string               `json:"resolved_account_id,omitempty"`
	ResolvedCategoryID        string               `json:"resolved_category_id,omitempty"`
	DuplicateMatch            *duplicateMatchView  `json:"duplicate_match,omitempty"`
	TransferCandidateRecordID string               `json:"transfer_candidate_record_id,omitempty"`
	OccurrenceMatch           *occurrenceMatchView `json:"occurrence_match,omitempty"`
	Status                    string               `json:"status"`
	TransactionID             string               `json:"transaction_id,omitempty"`
	SortOrder                 int                  `json:"sort_order"`
}

func importRecordViewFrom(r app.ResolveImportRecordResult) importRecordView {
	rec := r.Record
	v := importRecordView{
		ID:            rec.ID(),
		ImportBatchID: rec.ImportBatchID(),
		BookedDate:    rec.BookedDate().String(),
		Description:   rec.Description(),
		Amount:        rec.Amount().AmountString(),
		Currency:      rec.Amount().Currency(),
		Status:        string(rec.Status()),
		SortOrder:     rec.SortOrder(),
	}
	if d, ok := rec.PostedDate(); ok {
		v.PostedDate = d.String()
	}
	if id, ok := rec.ExternalID(); ok {
		v.ExternalID = id
	}
	if id, ok := rec.ResolvedAccountID(); ok {
		v.ResolvedAccountID = id
	}
	if id, ok := rec.ResolvedCategoryID(); ok {
		v.ResolvedCategoryID = id
	}
	if dm, ok := rec.DuplicateMatch(); ok {
		v.DuplicateMatch = &duplicateMatchView{
			Tier: string(dm.Tier()), MatchedTransactionID: dm.MatchedTransactionID(), Resolution: string(dm.Resolution()),
		}
	}
	if id, ok := rec.TransferCandidateRecordID(); ok {
		v.TransferCandidateRecordID = id
	}
	if om, ok := rec.OccurrenceMatch(); ok {
		v.OccurrenceMatch = &occurrenceMatchView{OccurrenceID: om.OccurrenceID(), Resolution: string(om.Resolution())}
	}
	if id, ok := rec.TransactionID(); ok {
		v.TransactionID = id
	}
	return v
}

// occurrenceMatchResolutionViewFrom builds an importRecordView from
// ResolveImportRecordOccurrenceMatch's own result shape, reusing
// importRecordViewFrom by re-wrapping its Record — the same
// already-established pattern StageImport/ListImportRecords use to share
// this view builder across every command that produces an ImportRecord.
func occurrenceMatchResolutionViewFrom(r app.ResolveImportRecordOccurrenceMatchResult) importRecordView {
	return importRecordViewFrom(app.ResolveImportRecordResult{Record: r.Record})
}

// flagDescription is what printImportRecordTable's own FLAGS column
// shows for one record — every review-relevant thing about it in one
// short word list, rather than a table wide enough for every field.
func flagDescription(v importRecordView) string {
	flags := ""
	if v.DuplicateMatch != nil {
		dm := v.DuplicateMatch
		switch {
		case dm.Tier == "exact":
			flags += "exact-duplicate "
		case dm.Resolution == "pending":
			flags += "suspected-duplicate "
		case dm.Resolution == "confirmed_duplicate":
			flags += "confirmed-duplicate "
		case dm.Resolution == "not_duplicate":
			flags += "not-a-duplicate "
		}
	}
	if v.TransferCandidateRecordID != "" {
		flags += "possible-transfer "
	}
	if v.OccurrenceMatch != nil {
		switch v.OccurrenceMatch.Resolution {
		case "pending":
			flags += "matched-occurrence "
		case "materialized":
			flags += "occurrence-materialized "
		case "dismissed":
			flags += "occurrence-match-dismissed "
		}
	}
	if flags == "" {
		return "-"
	}
	return flags[:len(flags)-1]
}

func printImportRecordTable(w io.Writer, views []importRecordView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No records staged.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDATE\tDESCRIPTION\tAMOUNT\tSTATUS\tFLAGS")
	for _, v := range views {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s %s\t%s\t%s\n", v.ID, v.BookedDate, v.Description, v.Amount, v.Currency, v.Status, flagDescription(v))
	}
	_ = tw.Flush()
}

// importCommitView is `import commit`'s own result shape: the committed
// import and the transactions it just created.
type importCommitView struct {
	Batch        importBatchView   `json:"batch"`
	Transactions []transactionView `json:"transactions"`
}

func importCommitViewFrom(r app.CommitImportBatchResult) importCommitView {
	v := importCommitView{Batch: importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch})}
	for _, txn := range r.Transactions {
		v.Transactions = append(v.Transactions, transactionViewFrom(app.TransactionResult{Transaction: txn}))
	}
	return v
}

// importRollbackView is `import rollback`'s own result shape: the
// rolled-back import and the IDs of the transactions it deleted.
type importRollbackView struct {
	Batch          importBatchView `json:"batch"`
	TransactionIDs []string        `json:"transaction_ids"`
}

func importRollbackViewFrom(r app.RollbackImportBatchResult) importRollbackView {
	return importRollbackView{
		Batch:          importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch}),
		TransactionIDs: r.TransactionIDs,
	}
}

// newImportCmd builds the "import" command group: upload, list, show,
// records, resolve, commit, and rollback — the surface issue #212 wires
// onto StageImport (#210), CommitImportBatch/RollbackImportBatch (#211),
// and this issue's own review use cases (internal/app/import_review.go).
// "suggest" (issue #306) is this group's one read-only addition: it wires
// onto SuggestForImportBatch (internal/app/import_suggest.go), a separate,
// explicitly-invoked action rather than something upload/records triggers
// on its own — ADR-0015's "a suggestion run is a user pressing a button."
func newImportCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Upload a file, review what it found, and commit or undo it",
	}
	cmd.AddCommand(
		newImportUploadCmd(factory),
		newImportListCmd(factory),
		newImportShowCmd(factory),
		newImportRecordsCmd(factory),
		newImportSuggestCmd(factory),
		newImportResolveCmd(factory),
		newImportResolveOccurrenceCmd(factory),
		newImportCommitCmd(factory),
		newImportRollbackCmd(factory),
	)
	return cmd
}

// suggestionSource is what every typesafe.ai suggestion this package
// renders is attributed to (issue #306, ADR-0015, docs/ux-principles.md
// §6). A literal, not something read off app.ImportRowSuggestion:
// ports.SuggestionProvider deliberately carries no Name() (nothing it
// returns is persisted, so there's no provenance column to fill), and an
// instance has only ever one configured provider for a suggestion to have
// come from.
const suggestionSource = "typesafe.ai"

// suggestedCategoryView is one staged row's surviving category
// suggestion (app.SuggestedCategory) — a proposal, never a value.
type suggestedCategoryView struct {
	CategoryID string  `json:"category_id"`
	Confidence float64 `json:"confidence"`
}

// suggestedOccurrenceView is one staged row's surviving occurrence-match
// suggestion (app.SuggestedOccurrence).
type suggestedOccurrenceView struct {
	OccurrenceID string  `json:"occurrence_id"`
	Confidence   float64 `json:"confidence"`
}

// importRowSuggestionView is one staged row's suggestion(s)
// (app.ImportRowSuggestion) — Category and/or Occurrence are omitted
// when there was nothing worth showing for that half of the row.
type importRowSuggestionView struct {
	RecordID   string                   `json:"record_id"`
	Source     string                   `json:"source"`
	Category   *suggestedCategoryView   `json:"category,omitempty"`
	Occurrence *suggestedOccurrenceView `json:"occurrence,omitempty"`
}

func importRowSuggestionViewFrom(s app.ImportRowSuggestion) importRowSuggestionView {
	v := importRowSuggestionView{RecordID: s.RecordID, Source: suggestionSource}
	if s.Category != nil {
		v.Category = &suggestedCategoryView{CategoryID: s.Category.CategoryID, Confidence: s.Category.Confidence}
	}
	if s.Occurrence != nil {
		v.Occurrence = &suggestedOccurrenceView{OccurrenceID: s.Occurrence.OccurrenceID, Confidence: s.Occurrence.Confidence}
	}
	return v
}

// importSuggestionsView is `import suggest`'s own result shape
// (app.SuggestForImportBatchResult).
type importSuggestionsView struct {
	Configured             bool                      `json:"configured"`
	Suggestions            []importRowSuggestionView `json:"suggestions"`
	TooManyCategoryOptions []string                  `json:"too_many_category_options,omitempty"`
	RowsSuggested          int                       `json:"rows_suggested"`
	RowsFailed             int                       `json:"rows_failed"`
	// FailureReason is why RowsFailed rows got no answer -- one of
	// typesafe.ai's own stable reason strings ("credential_rejected",
	// "throttled", "provider_unreachable"), empty when RowsFailed is 0.
	// Kept raw and machine-readable here (--json), the same way
	// TooManyCategoryOptions is a list of IDs rather than prose;
	// printImportSuggestions translates it into plain English for the
	// human-readable table.
	FailureReason string `json:"failure_reason,omitempty"`
}

func importSuggestionsViewFrom(r app.SuggestForImportBatchResult) importSuggestionsView {
	v := importSuggestionsView{
		Configured:             r.Configured,
		TooManyCategoryOptions: r.TooManyCategoryOptions,
		RowsSuggested:          r.RowsSuggested,
		RowsFailed:             r.RowsFailed,
		FailureReason:          r.FailureReason,
	}
	for _, s := range r.Suggestions {
		v.Suggestions = append(v.Suggestions, importRowSuggestionViewFrom(s))
	}
	return v
}

// explainFailureReason translates one of typesafe.ai's stable failure
// reason strings (internal/adapters/typesafe/errors.go's
// mapStatusError/mapTransportError — "credential_rejected", "throttled",
// "provider_unreachable") into the plain-English clause
// printImportSuggestions incorporates into its terminal message, matching
// the tone of that package's own Explain(...) strings without printing
// the raw code verbatim. A reason this CLI hasn't been taught yet (the
// closed set grows without this switch being updated) falls back to a
// generic clause rather than leaking the raw string into user-facing
// copy.
func explainFailureReason(reason string) string {
	switch reason {
	case "credential_rejected":
		return "your typesafe.ai credential was rejected"
	case "throttled":
		return "typesafe.ai is rate-limiting this instance right now"
	case "provider_unreachable":
		return "typesafe.ai couldn't be reached"
	default:
		return "typesafe.ai didn't answer"
	}
}

// printImportSuggestions renders `import suggest`'s human-readable
// output. No raw confidence decimal appears here (docs/ux-principles.md
// §6's "progressive disclosure": confidence is for ordering only in
// human output — the precise value is in --json) — Suggestions is
// already ranked by confidence, best first, so the table's own row order
// carries that information without printing the number.
func printImportSuggestions(w io.Writer, v importSuggestionsView) {
	if !v.Configured {
		_, _ = fmt.Fprintln(w, "Suggestions aren't set up on this instance. An operator can turn them on by setting "+
			"BODGER_TYPESAFE_API_KEY (see the user guide) — note that doing so sends this import's transaction "+
			"descriptions and amounts to typesafe.ai.")
		return
	}

	if len(v.Suggestions) == 0 {
		if v.RowsFailed > 0 {
			if v.FailureReason != "" {
				_, _ = fmt.Fprintf(w, "Asked typesafe.ai about %d row(s), but got no answer back — %s. Try again shortly.\n",
					v.RowsFailed, explainFailureReason(v.FailureReason))
			} else {
				_, _ = fmt.Fprintf(w, "Asked typesafe.ai about %d row(s), but got no answer back — try again shortly.\n", v.RowsFailed)
			}
		} else {
			_, _ = fmt.Fprintln(w, "No suggestions for this import right now.")
		}
	} else {
		_, _ = fmt.Fprintf(w, "%d suggestion(s) from typesafe.ai, ranked by confidence. These are proposals only — "+
			"nothing has been written. Accept one by committing the import and setting the resulting transaction's "+
			"category yourself, or by resolving the matched occurrence.\n", len(v.Suggestions))
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "RECORD\tSUGGESTED CATEGORY\tSUGGESTED OCCURRENCE")
		for _, s := range v.Suggestions {
			cat, occ := "-", "-"
			if s.Category != nil {
				cat = s.Category.CategoryID
			}
			if s.Occurrence != nil {
				occ = s.Occurrence.OccurrenceID
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.RecordID, cat, occ)
		}
		_ = tw.Flush()
		if v.RowsFailed > 0 {
			if v.FailureReason != "" {
				_, _ = fmt.Fprintf(w, "%d more row(s) were asked about but got no answer back from typesafe.ai — %s.\n",
					v.RowsFailed, explainFailureReason(v.FailureReason))
			} else {
				_, _ = fmt.Fprintf(w, "%d more row(s) were asked about but got no answer back from typesafe.ai this time.\n", v.RowsFailed)
			}
		}
	}

	if len(v.TooManyCategoryOptions) > 0 {
		_, _ = fmt.Fprintf(w, "%d row(s) got no category suggestion: you have too many categories of that kind for "+
			"typesafe.ai to choose between.\n", len(v.TooManyCategoryOptions))
	}
}

// newImportSuggestCmd builds "import suggest": issue #306's CLI surface
// for SuggestForImportBatch (internal/app/import_suggest.go, issue #305)
// — ADR-0015's advisory category/occurrence suggestions for a staged
// import's still-unresolved rows. This command writes nothing: it's a
// separate, explicitly-invoked read, the same shape "fx rates fetch"
// already established for a metered, user-triggered network call.
func newImportSuggestCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "suggest <import-id>",
		Short: "Ask typesafe.ai for category and occurrence-match suggestions on an import's still-unresolved rows",
		Long: "Suggestions are advisory only — nothing is written to your accounts, categories, or transactions by " +
			"this command. Each suggestion is attributed to typesafe.ai by name and ranked by its own confidence; " +
			"accepting one is exactly the same action you'd take without a suggestion — commit the import and set " +
			"the resulting transaction's category, or resolve the matched occurrence. If this instance has no " +
			"typesafe.ai key configured, this reports that plainly rather than failing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
				ActorID: ports.SeededUserID, ImportBatchRef: args[0],
			})
			if err != nil {
				return err
			}
			view := importSuggestionsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printImportSuggestions(w, view) })
		},
	}
}

type importUploadFlags struct {
	account           string
	filename          string
	format            string
	dateColumn        string
	descriptionColumn string
	amountColumn      string
	postedDateColumn  string
	currencyColumn    string
	externalIDColumn  string
	categoryColumn    string
}

func newImportUploadCmd(factory ServiceFactory) *cobra.Command {
	var f importUploadFlags
	cmd := &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a file and stage it for review",
		Long: "Nothing is written to your accounts, categories, or transactions by this command: every row is only " +
			"staged for review (see `bodger import records`), and stays that way until you commit it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read %s: %w", args[0], err)
			}
			filename := f.filename
			if filename == "" {
				filename = filepath.Base(args[0])
			}

			result, err := svc.StageImport(ctx, app.StageImportCommand{
				ActorID: ports.SeededUserID, AccountRef: f.account, Filename: filename, SourceFormat: f.format,
				FileContent: data,
				ColumnMapping: importparse.ColumnMapping{
					DateColumn:        f.dateColumn,
					DescriptionColumn: f.descriptionColumn,
					AmountColumn:      f.amountColumn,
					PostedDateColumn:  f.postedDateColumn,
					CurrencyColumn:    f.currencyColumn,
					ExternalIDColumn:  f.externalIDColumn,
					CategoryColumn:    f.categoryColumn,
				},
			})
			if err != nil {
				return err
			}

			batchView := importBatchViewFrom(app.GetImportBatchResult{Batch: result.Batch})
			recordViews := make([]importRecordView, 0, len(result.Records))
			for _, rec := range result.Records {
				recordViews = append(recordViews, importRecordViewFrom(app.ResolveImportRecordResult{Record: rec}))
			}
			return render(cmd, struct {
				Batch   importBatchView    `json:"batch"`
				Records []importRecordView `json:"records"`
			}{batchView, recordViews}, func(w io.Writer) {
				var ready, pending, excluded int
				for _, v := range recordViews {
					switch v.Status {
					case "ready":
						ready++
					case "pending":
						pending++
					case "excluded":
						excluded++
					}
				}
				_, _ = fmt.Fprintf(w, "Staged %d record(s) into import %s.\n", len(recordViews), batchView.ID)
				_, _ = fmt.Fprintf(w, "%d ready to commit, %d excluded as duplicates, %d awaiting your review.\n", ready, excluded, pending)
				if pending > 0 {
					_, _ = fmt.Fprintf(w, "Review them with `bodger import records %s`, then resolve each with `bodger import resolve`.\n", batchView.ID)
				}
			})
		},
	}
	cmd.Flags().StringVar(&f.account, "account", "", "the account new transactions from this import will post against (required)")
	cmd.Flags().StringVar(&f.filename, "filename", "", "the name to record for this file (defaults to the given path's own base name)")
	cmd.Flags().StringVar(&f.format, "format", "", `the file's format; only "csv" is supported today (defaults to csv)`)
	cmd.Flags().StringVar(&f.dateColumn, "date-column", "", "the column holding each row's booked date (required for csv)")
	cmd.Flags().StringVar(&f.descriptionColumn, "description-column", "", "the column holding each row's description (required for csv)")
	cmd.Flags().StringVar(&f.amountColumn, "amount-column", "", "the column holding each row's signed amount (required for csv)")
	cmd.Flags().StringVar(&f.postedDateColumn, "posted-date-column", "", "the column holding each row's posted date, when the file distinguishes it from the booked date")
	cmd.Flags().StringVar(&f.currencyColumn, "currency-column", "", "the column holding each row's currency, when the file has more than one")
	cmd.Flags().StringVar(&f.externalIDColumn, "external-id-column", "", "the column holding each row's own ID from the source, used to detect an exact duplicate on a re-import")
	cmd.Flags().StringVar(&f.categoryColumn, "category-column", "", "the column holding a hint for each row's category")
	_ = cmd.MarkFlagRequired("account")
	return cmd
}

func newImportListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every import, most recently created first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListImportBatches(ctx, app.ListImportBatchesQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]importBatchView, 0, len(result.Batches))
			for _, b := range result.Batches {
				views = append(views, importBatchViewFrom(app.GetImportBatchResult{Batch: b}))
			}
			return render(cmd, views, func(w io.Writer) { printImportBatchTable(w, views) })
		},
	}
}

func newImportShowCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "show <import-id>",
		Short: "Look up one import's status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.GetImportBatch(ctx, app.GetImportBatchQuery{ActorID: ports.SeededUserID, ImportBatchRef: args[0]})
			if err != nil {
				return err
			}
			view := importBatchViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printImportBatch(w, view) })
		},
	}
}

func newImportRecordsCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "records <import-id>",
		Short: "List an import's staged records, with their duplicate/transfer/occurrence flags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListImportRecords(ctx, app.ListImportRecordsQuery{ActorID: ports.SeededUserID, ImportBatchRef: args[0]})
			if err != nil {
				return err
			}
			views := make([]importRecordView, 0, len(result.Records))
			for _, rec := range result.Records {
				views = append(views, importRecordViewFrom(app.ResolveImportRecordResult{Record: rec}))
			}
			return render(cmd, views, func(w io.Writer) { printImportRecordTable(w, views) })
		},
	}
}

func newImportResolveCmd(factory ServiceFactory) *cobra.Command {
	var resolution string
	cmd := &cobra.Command{
		Use:   "resolve <record-id>",
		Short: "Record your decision on a staged record's suspected duplicate",
		Long: `--resolution "confirmed_duplicate" excludes the record from commit; "not_duplicate" clears it for commit. ` +
			"Refused for a record with no suspected duplicate to resolve, or one already resolved.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
				ActorID: ports.SeededUserID, ImportRecordRef: args[0], Resolution: resolution,
			})
			if err != nil {
				return err
			}
			view := importRecordViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Record %s resolved: now %s.\n", view.ID, view.Status)
			})
		},
	}
	cmd.Flags().StringVar(&resolution, "resolution", "", `your decision: "confirmed_duplicate" or "not_duplicate" (required)`)
	_ = cmd.MarkFlagRequired("resolution")
	return cmd
}

func newImportResolveOccurrenceCmd(factory ServiceFactory) *cobra.Command {
	var resolution, occurrenceID string
	cmd := &cobra.Command{
		Use:   "resolve-occurrence <record-id>",
		Short: "Record your decision on a staged record's matched pending occurrence",
		Long: `--resolution "materialized" turns the matched occurrence into its own transaction (using its rule's ` +
			`current amount and date) and excludes this record from commit, since the occurrence's transaction now ` +
			`covers the same money. --resolution "dismissed" leaves the occurrence untouched and clears this record ` +
			"for commit. Refused for a record already resolved. For a record with no matched occurrence of its own " +
			"(one `bodger import suggest` proposed instead — see its own --json output for the occurrence id), pass " +
			"--occurrence with that ID; it's re-checked against this record's own amount, date, and currency before " +
			"anything happens.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
				ActorID: ports.SeededUserID, ImportRecordRef: args[0], Resolution: resolution, OccurrenceID: occurrenceID,
			})
			if err != nil {
				return err
			}
			view := occurrenceMatchResolutionViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Record %s resolved: now %s.\n", view.ID, view.Status)
				if result.Transaction != nil {
					_, _ = fmt.Fprintf(w, "Materialised the matched occurrence as %q for %s.\n",
						result.Transaction.Description(), result.Transaction.BookedDate().String())
				}
			})
		},
	}
	cmd.Flags().StringVar(&resolution, "resolution", "", `your decision: "materialized" or "dismissed" (required)`)
	cmd.Flags().StringVar(&occurrenceID, "occurrence", "",
		"the pending occurrence to resolve against, when this record has no matched occurrence of its own already")
	_ = cmd.MarkFlagRequired("resolution")
	return cmd
}

func newImportCommitCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "commit <import-id>",
		Short: "Commit a staged import: write its cleared records as real transactions",
		Long:  "Refused if any staged record is still awaiting a decision on a suspected duplicate. Every record is written in one all-or-nothing step.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: ports.SeededUserID, ImportBatchRef: args[0]})
			if err != nil {
				return err
			}
			view := importCommitViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Committed import %s: %d transaction(s) created.\n", view.Batch.ID, len(view.Transactions))
			})
		},
	}
}

func newImportRollbackCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <import-id>",
		Short: "Undo a committed import: delete the transactions it created",
		Long:  "Only a currently committed import can be rolled back. The deleted transactions keep their history, the same as deleting any other transaction.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: ports.SeededUserID, ImportBatchRef: args[0]})
			if err != nil {
				return err
			}
			view := importRollbackViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Rolled back import %s: %d transaction(s) deleted.\n", view.Batch.ID, len(view.TransactionIDs))
			})
		},
	}
}
