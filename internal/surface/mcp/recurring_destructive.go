// This file implements issue #281's destructive-tier MCP wiring for
// recurring rules: archive_recurring_rule, wired onto
// internal/app/recurring_rules.go's ArchiveRecurringRule (issue #277,
// extended by #278 to cancel a newly-archived rule's still-pending
// occurrences) — following transactions_destructive.go's own
// confirmation-token tool conventions (ADR-0013).
//
// Tier decision: unlike archiveBudgetTool (registered write, not
// destructive -- see its own doc comment), archiving a recurring rule
// really does delete data: ArchiveRecurringRule calls
// ScheduledOccurrences.DeletePending, which removes every still-pending
// occurrence row outright rather than leaving it queryable (there is no
// "cancelled" OccurrenceStatus -- recurring_rules.go's own doc comment on
// ArchiveRecurringRule). That's the same "removes data, not just hides
// it" shape ADR-0013 scopes "destructive" to, and issue #281's own MCP
// bullet says so explicitly ("destructive tier (archive/delete rule)"),
// unlike issue #261's budget-archive tier decision.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// archiveRecurringRuleArgs is archive_recurring_rule's arguments.
type archiveRecurringRuleArgs struct {
	RuleID string `json:"rule_id"`
}

func archiveRecurringRuleInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rule_id": map[string]any{
				"type":        "string",
				"description": "The rule's ID.",
			},
		},
		"required":             []string{"rule_id"},
		"additionalProperties": false,
	}
}

// describeArchiveRecurringRule renders archive_recurring_rule's
// confirmation-token description (ADR-0013: "in the same terms the
// tool's own result would report"), by looking the rule and its pending
// occurrences up read-only first -- ListRecurringRules/ListScheduledOccurrences,
// never ArchiveRecurringRule itself -- so Describe never has to guess at
// what it's about to cancel.
func describeArchiveRecurringRule(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (string, error) {
	var args archiveRecurringRuleArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}

	rule, err := svc.GetRecurringRule(ctx, app.GetRecurringRuleQuery{ActorID: actorID, RuleID: args.RuleID})
	if err != nil {
		return "", err
	}
	view, err := recurringRuleViewFrom(rule)
	if err != nil {
		return "", err
	}
	if view.Archived {
		return fmt.Sprintf("Rule %s (%q) is already archived; this call would be a no-op.", view.ID, view.Description), nil
	}

	pending, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
		ActorID: actorID, RuleID: args.RuleID, Status: "pending",
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"This will archive rule %s (%q, %s %s): it stops generating any further occurrences, and its %d currently pending occurrence(s) are cancelled (removed) rather than left dangling. Already materialised or skipped occurrences are untouched and remain fully queryable.",
		view.ID, view.Description, view.Amount, view.Currency, len(pending.Occurrences),
	), nil
}

// archiveRecurringRuleTool wires app.Service.ArchiveRecurringRule onto a
// destructive-tier MCP tool.
func archiveRecurringRuleTool() ToolDef {
	return ToolDef{
		Name:        "archive_recurring_rule",
		Description: "Archive a recurring rule: it stops generating any further occurrences, and its still-pending occurrences are cancelled. Requires confirmation: call once to see what would be archived/cancelled, then again with the returned confirmation_token to actually do it.",
		Tier:        ports.MCPToolTierDestructive,
		InputSchema: archiveRecurringRuleInputSchema(),
		Describe:    describeArchiveRecurringRule,
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args archiveRecurringRuleArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: actorID, RuleID: args.RuleID})
			if err != nil {
				return errorResult(err), nil
			}
			return recurringRuleResultResult(result)
		},
	}
}
