package sqlite

import (
	"context"
	"database/sql"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// MCPToolCallRepository implements ports.MCPToolCallRepository over a *DB.
type MCPToolCallRepository struct {
	db *DB
}

// NewMCPToolCallRepository constructs an MCPToolCallRepository backed by db.
func NewMCPToolCallRepository(db *DB) *MCPToolCallRepository {
	return &MCPToolCallRepository{db: db}
}

var _ ports.MCPToolCallRepository = (*MCPToolCallRepository)(nil)

// Create implements ports.MCPToolCallRepository.
func (r *MCPToolCallRepository) Create(ctx context.Context, actorID string, call ports.MCPToolCall) error {
	if err := requireActor(actorID, call.UserID); err != nil {
		return err
	}

	_, err := r.db.write.ExecContext(ctx, `
		INSERT INTO mcp_tool_call (id, user_id, tool_name, tier, arguments, confirmation_token, result, called_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		call.ID, actorID, call.ToolName, string(call.Tier), call.Arguments,
		nullableConfirmationToken(call.ConfirmationToken), call.Result, formatTime(call.CalledAt),
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't record MCP tool call %q.", call.ToolName)
	}
	return nil
}

// List implements ports.MCPToolCallRepository.
func (r *MCPToolCallRepository) List(ctx context.Context, actorID string, limit int) ([]ports.MCPToolCall, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, tool_name, tier, arguments, confirmation_token, result, called_at
		FROM mcp_tool_call
		WHERE user_id = ?
		ORDER BY called_at DESC, id DESC
		LIMIT ?
	`, actorID, limit)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var calls []ports.MCPToolCall
	for rows.Next() {
		call, err := scanMCPToolCall(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return calls, nil
}

func scanMCPToolCall(row rowScanner) (ports.MCPToolCall, error) {
	var (
		id, userID, toolName, tier, arguments, result string
		confirmationTokenCol                          sql.NullString
		calledAtCol                                   string
	)
	if err := row.Scan(&id, &userID, &toolName, &tier, &arguments, &confirmationTokenCol, &result, &calledAtCol); err != nil {
		return ports.MCPToolCall{}, err
	}

	calledAt, err := parseTime(calledAtCol)
	if err != nil {
		return ports.MCPToolCall{}, err
	}

	return ports.MCPToolCall{
		ID:                id,
		UserID:            userID,
		ToolName:          toolName,
		Tier:              ports.MCPToolTier(tier),
		Arguments:         arguments,
		ConfirmationToken: parseNullableConfirmationToken(confirmationTokenCol),
		Result:            result,
		CalledAt:          calledAt,
	}, nil
}

// nullableConfirmationToken and parseNullableConfirmationToken convert
// MCPToolCall.ConfirmationToken (an optional *string — nil for a
// write-tier call, or a destructive call's issuing leg) to and from the
// nullable TEXT column, mirroring nullableTimeValue/parseNullableTime's
// shape (timestamps.go) for a plain string rather than a time.Time.
func nullableConfirmationToken(token *string) sql.NullString {
	if token == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *token, Valid: true}
}

func parseNullableConfirmationToken(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}
