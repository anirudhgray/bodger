package app_test

import (
	"context"
	"sort"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// memAccounts, memCategories, memTransactions, and memTags are in-memory
// ports implementations for issue #6's use-case tests - "use-case tests
// run against in-memory repositories with a frozen clock and a fixed
// timezone" (issue #6's "done when" list). They're distinct from
// service_test.go's fakeAccounts/fakeCategories/fakeTransactions/fakeTags,
// which are no-op stand-ins good enough for NewService's wiring tests but
// not for exercising real create/get/list/update behaviour.
//
// Every method enforces the same actor-scoping contract the real
// internal/adapters/sqlite repositories document (ADR-0006): a method
// never returns or mutates another actor's rows. This matters for these
// tests specifically - it's what proves the app layer's own authorisation
// checks (resolving a ref only against the caller's own List results)
// actually can't cross into another actor's data, not just that the
// fixture never provides another actor's data to leak.

type memAccounts struct {
	byID map[string]ledger.Account
}

func newMemAccounts() *memAccounts { return &memAccounts{byID: map[string]ledger.Account{}} }

func (m *memAccounts) Create(_ context.Context, actorID string, a ledger.Account) error {
	if a.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	for _, existing := range m.byID {
		if existing.UserID() == actorID && existing.Name() == a.Name() {
			return errs.New(errs.Conflict).Explain("An account named %q already exists.", a.Name())
		}
	}
	m.byID[a.ID()] = a
	return nil
}

func (m *memAccounts) Get(_ context.Context, actorID, id string) (ledger.Account, error) {
	a, ok := m.byID[id]
	if !ok || a.UserID() != actorID {
		return ledger.Account{}, errs.New(errs.NotFound).Explain("No account with ID %q.", id).Field("id")
	}
	return a, nil
}

func (m *memAccounts) List(_ context.Context, actorID string) ([]ledger.Account, error) {
	var out []ledger.Account
	for _, a := range m.byID {
		if a.UserID() == actorID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (m *memAccounts) Update(_ context.Context, actorID string, a ledger.Account) error {
	existing, ok := m.byID[a.ID()]
	if !ok || existing.UserID() != actorID {
		return errs.New(errs.NotFound).Explain("No account with ID %q.", a.ID()).Field("id")
	}
	if a.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	m.byID[a.ID()] = a
	return nil
}

var _ ports.AccountRepository = (*memAccounts)(nil)

type memCategories struct {
	byID map[string]ledger.Category
}

func newMemCategories() *memCategories { return &memCategories{byID: map[string]ledger.Category{}} }

func (m *memCategories) Create(_ context.Context, actorID string, c ledger.Category) error {
	if c.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	parentID, hasParent := c.ParentID()
	for _, existing := range m.byID {
		if existing.UserID() != actorID {
			continue
		}
		existingParentID, existingHasParent := existing.ParentID()
		if existingHasParent != hasParent || existingParentID != parentID {
			continue
		}
		if existing.Name() == c.Name() {
			return errs.New(errs.Conflict).Explain("A sibling category named %q already exists.", c.Name())
		}
	}
	m.byID[c.ID()] = c
	return nil
}

func (m *memCategories) Get(_ context.Context, actorID, id string) (ledger.Category, error) {
	c, ok := m.byID[id]
	if !ok || c.UserID() != actorID {
		return ledger.Category{}, errs.New(errs.NotFound).Explain("No category with ID %q.", id).Field("id")
	}
	return c, nil
}

func (m *memCategories) List(_ context.Context, actorID string) ([]ledger.Category, error) {
	var out []ledger.Category
	for _, c := range m.byID {
		if c.UserID() == actorID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (m *memCategories) Update(_ context.Context, actorID string, c ledger.Category) error {
	existing, ok := m.byID[c.ID()]
	if !ok || existing.UserID() != actorID {
		return errs.New(errs.NotFound).Explain("No category with ID %q.", c.ID()).Field("id")
	}
	if c.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	m.byID[c.ID()] = c
	return nil
}

var _ ports.CategoryRepository = (*memCategories)(nil)

// memTransactionRecord bundles a stored transaction with its tags and
// insertion sequence number, which stands in for created_at as the second
// tiebreak in the fully-specified sort order (booked_date DESC, created_at
// DESC, id DESC) - creation order and created_at agree for this fake since
// nothing here batches multiple creates under one identical timestamp.
type memTransactionRecord struct {
	txn  ledger.Transaction
	tags []ledger.Tag
	seq  int
}

type memTransactions struct {
	byID map[string]memTransactionRecord
	next int
	// categories backs CategoryID subtree expansion in List, mirroring
	// the real adapter's recursive CTE (ADR-0009: "CategoryRefs includes
	// the subtree by default"). It may be nil, in which case CategoryID
	// filtering degrades to an exact match only - fine for tests that
	// don't exercise category filtering at all.
	categories *memCategories
}

func newMemTransactions(categories *memCategories) *memTransactions {
	return &memTransactions{byID: map[string]memTransactionRecord{}, categories: categories}
}

// categorySubtreeIDs returns rootID plus every descendant's ID, computed
// by repeatedly sweeping every category looking for a parent already in
// the set - simple and quadratic, which is fine for a test fixture's
// category counts.
func (m *memTransactions) categorySubtreeIDs(rootID string) map[string]bool {
	ids := map[string]bool{rootID: true}
	if m.categories == nil {
		return ids
	}
	for {
		grew := false
		for _, c := range m.categories.byID {
			if ids[c.ID()] {
				continue
			}
			if parentID, ok := c.ParentID(); ok && ids[parentID] {
				ids[c.ID()] = true
				grew = true
			}
		}
		if !grew {
			return ids
		}
	}
}

func (m *memTransactions) Create(_ context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error {
	if txn.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	m.next++
	m.byID[txn.ID()] = memTransactionRecord{txn: txn, tags: tags, seq: m.next}
	return nil
}

func (m *memTransactions) Get(_ context.Context, actorID, id string) (ledger.Transaction, []ledger.Tag, error) {
	rec, ok := m.byID[id]
	if !ok || rec.txn.UserID() != actorID || rec.txn.IsDeleted() {
		return ledger.Transaction{}, nil, errs.New(errs.NotFound).Explain("No transaction with ID %q.", id).Field("id")
	}
	return rec.txn, rec.tags, nil
}

func (m *memTransactions) List(_ context.Context, actorID string, filter ports.TransactionFilter) ([]ledger.Transaction, error) {
	var matches []memTransactionRecord
	for _, rec := range m.byID {
		if rec.txn.UserID() != actorID || rec.txn.IsDeleted() {
			continue
		}
		if filter.Kind != "" && rec.txn.Kind() != filter.Kind {
			continue
		}
		if filter.FromDate != nil && rec.txn.BookedDate().Before(*filter.FromDate) {
			continue
		}
		if filter.ToDate != nil && rec.txn.BookedDate().After(*filter.ToDate) {
			continue
		}
		if filter.AccountID != "" && !hasPostingOnAccount(rec.txn, filter.AccountID) {
			continue
		}
		if filter.CategoryID != "" && !hasPostingInCategorySet(rec.txn, m.categorySubtreeIDs(filter.CategoryID)) {
			continue
		}
		matches = append(matches, rec)
	}

	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if !a.txn.BookedDate().Equal(b.txn.BookedDate()) {
			return a.txn.BookedDate().After(b.txn.BookedDate())
		}
		if a.seq != b.seq {
			return a.seq > b.seq
		}
		return a.txn.ID() > b.txn.ID()
	})

	if filter.Offset > 0 {
		if filter.Offset >= len(matches) {
			matches = nil
		} else {
			matches = matches[filter.Offset:]
		}
	}
	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}

	out := make([]ledger.Transaction, len(matches))
	for i, rec := range matches {
		out[i] = rec.txn
	}
	return out, nil
}

func (m *memTransactions) Update(_ context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error {
	existing, ok := m.byID[txn.ID()]
	if !ok || existing.txn.UserID() != actorID || existing.txn.IsDeleted() {
		return errs.New(errs.NotFound).Explain("No transaction with ID %q.", txn.ID()).Field("id")
	}
	if txn.UserID() != actorID {
		return errs.New(errs.NotAllowed)
	}
	m.byID[txn.ID()] = memTransactionRecord{txn: txn, tags: tags, seq: existing.seq}
	return nil
}

var _ ports.TransactionRepository = (*memTransactions)(nil)

func hasPostingOnAccount(txn ledger.Transaction, accountID string) bool {
	for _, p := range txn.Postings() {
		if p.AccountID() == accountID {
			return true
		}
	}
	return false
}

func hasPostingInCategorySet(txn ledger.Transaction, categoryIDs map[string]bool) bool {
	for _, p := range txn.Postings() {
		if id, ok := p.CategoryID(); ok && categoryIDs[id] {
			return true
		}
	}
	return false
}

type memTags struct {
	values map[string]map[string]bool // actorID -> value -> true
}

func newMemTags() *memTags { return &memTags{values: map[string]map[string]bool{}} }

func (m *memTags) List(_ context.Context, actorID string) ([]ledger.Tag, error) {
	var values []string
	for v := range m.values[actorID] {
		values = append(values, v)
	}
	sort.Strings(values)
	out := make([]ledger.Tag, 0, len(values))
	for _, v := range values {
		t, err := ledger.NewTag(v)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

var _ ports.TagRepository = (*memTags)(nil)

// memUsers, memSessions, and memAPITokens are issue #55's in-memory ports
// implementations, mirroring the fakes above: enough to exercise real
// Login/Logout/SetPassword/API-token behaviour, still enforcing the
// actor-scoping contract the real internal/adapters/sqlite repositories
// document (ADR-0006).

type memUsers struct {
	byID map[string]ports.User
}

func newMemUsers() *memUsers { return &memUsers{byID: map[string]ports.User{}} }

// newMemUsersSeeded mirrors the real first migration's behaviour
// (00001_create_users.sql): a fresh database always has exactly one user
// row, ports.SeededUserID, with no password set yet. Auth use-case tests
// build on this the same way a real fresh install does — SetPassword
// before the first Login.
func newMemUsersSeeded() *memUsers {
	m := newMemUsers()
	m.byID[ports.SeededUserID] = ports.User{ID: ports.SeededUserID}
	return m
}

func (m *memUsers) GetByID(_ context.Context, id string) (ports.User, error) {
	u, ok := m.byID[id]
	if !ok {
		return ports.User{}, errs.New(errs.NotFound).Explain("No user with ID %q.", id).Field("id")
	}
	return u, nil
}

func (m *memUsers) SetPasswordHash(_ context.Context, actorID, passwordHash string) error {
	u, ok := m.byID[actorID]
	if !ok {
		return errs.New(errs.NotFound).Explain("No user with ID %q.", actorID).Field("id")
	}
	u.PasswordHash = &passwordHash
	m.byID[actorID] = u
	return nil
}

func (m *memUsers) SetReportingCurrency(_ context.Context, actorID, currency string) error {
	u, ok := m.byID[actorID]
	if !ok {
		return errs.New(errs.NotFound).Explain("No user with ID %q.", actorID).Field("id")
	}
	u.ReportingCurrency = &currency
	m.byID[actorID] = u
	return nil
}

var _ ports.UserRepository = (*memUsers)(nil)

type memSessions struct {
	byID map[string]ports.Session
}

func newMemSessions() *memSessions { return &memSessions{byID: map[string]ports.Session{}} }

func (m *memSessions) Create(_ context.Context, actorID string, session ports.Session) error {
	if session.UserID != actorID {
		return errs.New(errs.NotAllowed)
	}
	for _, existing := range m.byID {
		if existing.TokenHash == session.TokenHash {
			return errs.New(errs.Conflict).Explain("A session with this token already exists.")
		}
	}
	m.byID[session.ID] = session
	return nil
}

func (m *memSessions) GetByTokenHash(_ context.Context, tokenHash string) (ports.Session, error) {
	for _, s := range m.byID {
		if s.TokenHash == tokenHash {
			return s, nil
		}
	}
	return ports.Session{}, errs.New(errs.NotFound).Explain("No session found.")
}

func (m *memSessions) Touch(_ context.Context, actorID, id string, lastUsedAt, expiresAt time.Time) error {
	s, ok := m.byID[id]
	if !ok || s.UserID != actorID {
		return errs.New(errs.NotFound).Explain("No session with ID %q.", id).Field("id")
	}
	s.LastUsedAt = lastUsedAt
	s.ExpiresAt = expiresAt
	m.byID[id] = s
	return nil
}

func (m *memSessions) Delete(_ context.Context, actorID, id string) error {
	s, ok := m.byID[id]
	if !ok || s.UserID != actorID {
		return errs.New(errs.NotFound).Explain("No session with ID %q.", id).Field("id")
	}
	delete(m.byID, id)
	return nil
}

func (m *memSessions) ListByUser(_ context.Context, actorID string) ([]ports.Session, error) {
	var out []ports.Session
	for _, s := range m.byID {
		if s.UserID == actorID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (m *memSessions) DeleteAllByUser(_ context.Context, actorID string) error {
	for id, s := range m.byID {
		if s.UserID == actorID {
			delete(m.byID, id)
		}
	}
	return nil
}

var _ ports.SessionRepository = (*memSessions)(nil)

type memAPITokens struct {
	byID map[string]ports.APIToken
}

func newMemAPITokens() *memAPITokens { return &memAPITokens{byID: map[string]ports.APIToken{}} }

func (m *memAPITokens) Create(_ context.Context, actorID string, token ports.APIToken) error {
	if token.UserID != actorID {
		return errs.New(errs.NotAllowed)
	}
	for _, existing := range m.byID {
		if existing.TokenHash == token.TokenHash {
			return errs.New(errs.Conflict).Explain("A token with this value already exists.")
		}
	}
	m.byID[token.ID] = token
	return nil
}

func (m *memAPITokens) GetByTokenHash(_ context.Context, tokenHash string) (ports.APIToken, error) {
	for _, t := range m.byID {
		if t.TokenHash == tokenHash {
			return t, nil
		}
	}
	return ports.APIToken{}, errs.New(errs.NotFound).Explain("No API token found.")
}

func (m *memAPITokens) List(_ context.Context, actorID string) ([]ports.APIToken, error) {
	var out []ports.APIToken
	for _, t := range m.byID {
		if t.UserID == actorID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (m *memAPITokens) Touch(_ context.Context, actorID, id string, lastUsedAt time.Time) error {
	t, ok := m.byID[id]
	if !ok || t.UserID != actorID {
		return errs.New(errs.NotFound).Explain("No API token with ID %q.", id).Field("id")
	}
	t.LastUsedAt = &lastUsedAt
	m.byID[id] = t
	return nil
}

func (m *memAPITokens) Revoke(_ context.Context, actorID, id string, revokedAt time.Time) error {
	t, ok := m.byID[id]
	if !ok || t.UserID != actorID {
		return errs.New(errs.NotFound).Explain("No API token with ID %q.", id).Field("id")
	}
	t.RevokedAt = &revokedAt
	m.byID[id] = t
	return nil
}

var _ ports.APITokenRepository = (*memAPITokens)(nil)

// memFxRates is an in-memory ports.FxRateRepository, for whatever
// FX-management use case (a later issue) needs one wired into
// newTestService. Store/StoreBatch/Lookup mirror the real sqlite
// adapter's behaviour (upsert on (base, quote, date, source);
// fx.SelectRate does the actual selection — domain.Date's fields are all
// comparable, so it works directly as part of a map key). InUsePairs has
// no accounts or transactions of its own to inspect — that data lives in
// memAccounts/memTransactions, not here — so it always returns no pairs;
// nothing in this package's use-case tests calls it yet.
type memFxRateKey struct {
	base, quote, source string
	date                domain.Date
}

type memFxRates struct {
	byKey map[memFxRateKey]fx.Rate
}

func newMemFxRates() *memFxRates { return &memFxRates{byKey: map[memFxRateKey]fx.Rate{}} }

func (m *memFxRates) Store(_ context.Context, rate fx.Rate, date domain.Date, source string) error {
	m.byKey[memFxRateKey{rate.Base(), rate.Quote(), source, date}] = rate
	return nil
}

func (m *memFxRates) StoreBatch(ctx context.Context, rows []ports.FxRateRow) error {
	for _, row := range rows {
		if err := m.Store(ctx, row.Rate, row.Date, row.Source); err != nil {
			return err
		}
	}
	return nil
}

func (m *memFxRates) Lookup(_ context.Context, base, quote string, date domain.Date, windowDays int) (fx.Selection, error) {
	var candidates []fx.RateCandidate
	for key, rate := range m.byKey {
		if key.base != base || key.quote != quote {
			continue
		}
		candidates = append(candidates, fx.RateCandidate{Date: key.date, Rate: rate})
	}
	sel, err := fx.SelectRate(candidates, date, windowDays)
	if err != nil {
		return fx.Selection{}, errs.New(errs.NotFound).Explain("No %s/%s rate available.", base, quote).Wrap(err)
	}
	return sel, nil
}

func (m *memFxRates) InUsePairs(context.Context, string, string) ([]ports.CurrencyPair, error) {
	return nil, nil
}

var _ ports.FxRateRepository = (*memFxRates)(nil)
