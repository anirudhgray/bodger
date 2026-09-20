package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/version"
	"github.com/anirudhgray/bodger/internal/ports"
)

// requestBody is the exact, and only, shape a request to
// POST {baseURL}/v1/systemone can take. Every field is populated from one
// ports.SuggestionRow's own fields — never from the row's RecordID, never
// from a second row, and never from anything an importing.ImportRecord
// carries that SuggestionRow doesn't (RawPayload, account numbers,
// external IDs). See typesafe_pinned_test.go, which serialises this type
// and pins that shape: it is the only thing standing between this
// projection and someone later "simplifying" the builder into
// json.Marshal(row).
type requestBody struct {
	Model     string                  `json:"model"`
	State     stateBody               `json:"state"`
	Questions map[string]questionBody `json:"questions"`
}

// stateBody is one row's state, an object per the vendor's own preference
// ("state must be a string, JSON object, or array of text values, with
// objects preferred so each part has a descriptive name" — ADR-0015).
// Exactly four fields, matching ADR-0015's "What leaves the machine" table:
// description, amount, currency, and booked date. Nothing else.
type stateBody struct {
	Description string `json:"description"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Date        string `json:"date"`
}

// questionBody is one Choice question. "choice" is the only question type
// this adapter ever sends — ADR-0015 asks Score and Noul for nothing here.
type questionBody struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// buildRequestBody projects row into the wire shape above. ok is false
// when row has nothing worth asking about — no categories and no
// occurrence candidates — in which case the caller must not send a
// request at all (ADR-0015: "the model is never invited to invent a match
// out of an empty or irrelevant set").
func buildRequestBody(row ports.SuggestionRow) (requestBody, bool) {
	questions := make(map[string]questionBody, 2)

	if len(row.Categories) > 0 {
		criteria := make(map[string]string, len(row.Categories)+1)
		for _, opt := range row.Categories {
			criteria[opt.ID] = opt.Name
		}
		criteria[noneOfTheseOption] = "None of these categories apply to this transaction."
		questions[categoryQuestionID] = questionBody{
			Type:         "choice",
			Instructions: "Which of these categories does this transaction's description refer to? Choose \"none of these\" if none of them plausibly apply.",
			Criteria:     criteria,
		}
	}

	if len(row.OccurrenceCandidates) > 0 {
		criteria := make(map[string]string, len(row.OccurrenceCandidates)+1)
		for _, opt := range row.OccurrenceCandidates {
			criteria[opt.OccurrenceID] = opt.Description
		}
		criteria[noneOfTheseOption] = "None of these scheduled occurrences are the same payment as this transaction."
		questions[occurrenceQuestionID] = questionBody{
			Type:         "choice",
			Instructions: "These candidates are already known to match this transaction's amount and date. Which one, if any, does this transaction's description refer to? Choose \"none of these\" if none of them plausibly refer to the same payment.",
			Criteria:     criteria,
		}
	}

	if len(questions) == 0 {
		return requestBody{}, false
	}

	return requestBody{
		Model: Model,
		State: stateBody{
			Description: row.Description,
			Amount:      row.Amount.AmountString(),
			Currency:    row.Amount.Currency(),
			Date:        row.Date.String(),
		},
		Questions: questions,
	}, true
}

// answerBody is one question's answer in the response. Confidence is
// passed through to ports.RowSuggestion entirely unfiltered — ADR-0015
// puts the 0.5 display threshold in the application layer, not here.
type answerBody struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// responseBody is the response's shape. Usage is received but never used —
// nothing in this adapter or above it needs a token count today.
type responseBody struct {
	Answers map[string]answerBody `json:"answers"`
}

// suggestRow issues exactly one HTTP request for row and returns its
// suggestion. attempted is false when row had nothing worth asking (see
// buildRequestBody) — the row is silently skipped, not a failure.
func (c *Client) suggestRow(ctx context.Context, row ports.SuggestionRow) (ports.RowSuggestion, bool, error) {
	body, ok := buildRequestBody(row)
	if !ok {
		return ports.RowSuggestion{}, false, nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ports.RowSuggestion{}, true, errs.New(errs.Internal).
			Explain("could not build a request to the suggestion provider").
			Wrap(err)
	}

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", fmt.Sprintf("bodger/%s", version.Version))
		return req, nil
	})
	if err != nil {
		return ports.RowSuggestion{}, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ports.RowSuggestion{}, true, mapStatusError(resp.StatusCode)
	}

	var decoded responseBody
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ports.RowSuggestion{}, true, errs.New(errs.Internal).
			Explain("the suggestion provider returned a response bodger couldn't parse").
			Wrap(err)
	}

	return toRowSuggestion(row.RecordID, decoded), true, nil
}

// toRowSuggestion maps a decoded response onto row's RecordID. A missing
// answer, or a "none of these" choice, both map to an empty ID — "no
// answer", not an error (ADR-0015 / ports.RowSuggestion's doc comment).
func toRowSuggestion(recordID string, decoded responseBody) ports.RowSuggestion {
	out := ports.RowSuggestion{RecordID: recordID}

	if a, ok := decoded.Answers[categoryQuestionID]; ok && a.Choice != noneOfTheseOption {
		out.CategoryID = a.Choice
		out.CategoryConfidence = a.Confidence
	}
	if a, ok := decoded.Answers[occurrenceQuestionID]; ok && a.Choice != noneOfTheseOption {
		out.OccurrenceID = a.Choice
		out.OccurrenceConfidence = a.Confidence
	}
	return out
}

// drainAndClose discards resp's body and closes it, for the retry path
// where a response is read for its status only and must not leak a
// connection back to the pool half-read.
func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
