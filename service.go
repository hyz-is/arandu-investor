package investor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/translation"
	activitylog "github.com/hyz-is/arandu-activitylog"
	wallet "github.com/hyz-is/arandu-wallet"
)

// Pagination bounds for List. A request that asks for everything gets the
// maximum, never everything: an unbounded query is how one page load takes a
// production database down.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// sortableInvestor is the ordering allowlist. A column name taken directly
// from a request would turn ordering into an injection surface.
var sortableInvestor = map[string]string{
	"":           "created_at",
	"name":       "name",
	"created_at": "created_at",
}

// InvestorService orchestrates the use cases of this package: it validates the
// request, asks the policy for a Grant, applies the rules the entities hold for
// themselves, and persists with that Grant.
//
// It receives its collaborators through the constructor. There is no container
// and no resolution by reflection: what this service is made of is written at
// the one place that builds it, and reading that place is how somebody learns
// what the package touches.
//
// Everything a handler is allowed to do goes through here. The service is the
// only owner of the database handle, so the request layer cannot reach a Model
// before the policy has answered.
type InvestorService struct {
	db     *data.DB
	policy InvestorPolicy
	// now is the clock, a field so a test can hold it still. Every timestamp a
	// row carries is read from here, never from time.Now at the call site.
	now func() time.Time
	// files is where the signed contracts are kept: the Disk the application
	// passed in Config.Files, reached with the Grant so the tenant prefixes
	// every key.
	files *filesystem.Disk
	// loc is the application's timezone: the calendar an instant is turned into
	// a day in, which is what the periods count by.
	loc *time.Location
	// wallets is arandu-wallet's service, where the invoices are deposited and
	// withdrawn: the application's one instance, passed in Config.Wallet.
	wallets *wallet.WalletService
	// payer is who pays the investors, printed on the income report.
	payer Payer
	// activity is arandu-activitylog's logger, Config.Activity; nil records
	// nothing. describe turns an event into the sentence the entry carries.
	activity *activitylog.Logger
	describe func(event string) string
	// labels are the sentences a report is written in for a request: the
	// module's, in Config.Locale or the request's language. A service built
	// without the module writes them in the request's language from the
	// shipped catalogue.
	labels func(ctx context.Context) Labels
}

// labelsFor are the sentences of one request.
func (s *InvestorService) labelsFor(ctx context.Context) Labels {
	if s.labels != nil {
		return s.labels(ctx)
	}
	return Labels{Locale: translation.Locale(ctx)}
}

// location is the timezone days are counted in.
func (s *InvestorService) location() *time.Location {
	if s.loc == nil {
		return time.UTC
	}
	return s.loc
}

// NewInvestorService wires the service over the application's database handle.
func NewInvestorService(db *data.DB) *InvestorService {
	return &InvestorService{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// CreateRequest is the input contract for registering an investor.
//
// The fields are explicit and there is no mass assignment, so a request body
// cannot write a column nobody meant to expose. There is no TenantID here and
// there must never be one: the tenant comes from the Grant, which comes from
// the session.
type CreateRequest struct {
	// UserID is the login user who is this investor.
	UserID string
	// Kind is Person or Company.
	Kind PersonKind
	// Document is the CPF or the CNPJ, formatted or not.
	Document string
	// Name is the person's name, or the company's trade name.
	Name string
	// LegalName is the company's registered name. Required for a company.
	LegalName string
	// LegalRepresentative is who signs for the company. Required for a company.
	LegalRepresentative string
	// Email is where the investor is reached.
	Email string
}

// Validate reports the errors per field.
func (r CreateRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "user_id", r.UserID)
	validation.MaxLen(e, "user_id", r.UserID, maxIdentifierLen)
	validation.Required(e, "name", r.Name)
	validation.MaxLen(e, "name", r.Name, maxNameLen)
	validation.Required(e, "email", r.Email)
	validation.Email(e, "email", r.Email)
	validation.MaxLen(e, "email", r.Email, maxEmailLen)
	switch r.Kind {
	case Person:
	case Company:
		validation.Required(e, "legal_name", r.LegalName)
		validation.MaxLen(e, "legal_name", r.LegalName, maxNameLen)
		validation.Required(e, "legal_representative", r.LegalRepresentative)
		validation.MaxLen(e, "legal_representative", r.LegalRepresentative, maxNameLen)
	default:
		e.Add("person_kind", message("person_kind"))
	}
	if r.Kind == Person || r.Kind == Company {
		if _, err := normalizeDocument(r.Kind, r.Document); err != nil {
			e.Add("document", message("document", translation.Replace{"document": documentName(r.Kind)}))
		}
	}
	return e
}

func documentName(kind PersonKind) string {
	if kind == Company {
		return "CNPJ"
	}
	return "CPF"
}

// Compile-time proof that the request honors the validation contract.
var _ validation.Validatable = CreateRequest{}

// Create registers an investor: validate, authorize, then act with the Grant
// the authorization produced.
//
// The candidate is authorized before it is stored, and the candidate is what
// the policy sees -- so a rule about what may be created is a rule about the
// record being created, and not about the person alone.
func (s *InvestorService) Create(ctx context.Context, actor security.Subject, in CreateRequest) (*Investor, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	document, _ := normalizeDocument(in.Kind, in.Document)

	proposed := Investor{
		UserID:              strings.TrimSpace(in.UserID),
		Kind:                in.Kind,
		Document:            document,
		Name:                strings.TrimSpace(in.Name),
		LegalName:           strings.TrimSpace(in.LegalName),
		LegalRepresentative: strings.TrimSpace(in.LegalRepresentative),
		Email:               strings.TrimSpace(in.Email),
		Status:              InvestorActive,
	}
	if proposed.Kind == Person {
		proposed.LegalName, proposed.LegalRepresentative = "", ""
	}

	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, proposed)
	if err != nil {
		return nil, err
	}
	if proposed.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	// The row is built by the query so it is wired to its table, and its fields
	// are assigned one by one: copying proposed over it would replace the
	// embedded model with the unwired one of a struct literal.
	candidate, err := Investors(s.db).New()
	if err != nil {
		return nil, err
	}
	now := s.now()
	candidate.ID = proposed.ID
	candidate.TenantID = data.Tenant(g)
	candidate.UserID = proposed.UserID
	candidate.Kind = proposed.Kind
	candidate.Document = proposed.Document
	candidate.Name = proposed.Name
	candidate.LegalName = proposed.LegalName
	candidate.LegalRepresentative = proposed.LegalRepresentative
	candidate.Email = proposed.Email
	candidate.Status = proposed.Status
	candidate.CreatedAt = now
	candidate.UpdatedAt = now
	if _, err := candidate.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventInvestorCreated, "investor", candidate.ID, map[string]any{"name": candidate.Name, "user_id": candidate.UserID, "kind": string(candidate.Kind)}); err != nil {
		return nil, err
	}
	return candidate, nil
}

// UpdateRequest is what may change in an investor after it is registered.
// The kind and the document are not here: a person does not become a company,
// and a different document is a different investor.
type UpdateRequest struct {
	Name                string
	LegalName           string
	LegalRepresentative string
	Email               string
}

// Update changes the names and the e-mail of an investor.
func (s *InvestorService) Update(ctx context.Context, actor security.Subject, id string, in UpdateRequest) (*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *record); err != nil {
		return nil, err
	}

	e := validation.Errors{}
	validation.Required(e, "name", in.Name)
	validation.MaxLen(e, "name", in.Name, maxNameLen)
	validation.Required(e, "email", in.Email)
	validation.Email(e, "email", in.Email)
	if record.Kind == Company {
		validation.Required(e, "legal_name", in.LegalName)
		validation.Required(e, "legal_representative", in.LegalRepresentative)
	}
	if e.Any() {
		return nil, e
	}

	record.Name = strings.TrimSpace(in.Name)
	record.Email = strings.TrimSpace(in.Email)
	if record.Kind == Company {
		record.LegalName = strings.TrimSpace(in.LegalName)
		record.LegalRepresentative = strings.TrimSpace(in.LegalRepresentative)
	}
	record.UpdatedAt = s.now()
	if _, err := record.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventInvestorUpdated, "investor", record.ID, map[string]any{"name": record.Name, "email": record.Email}); err != nil {
		return nil, err
	}
	return record, nil
}

// SetActive suspends or reactivates an investor. A suspended investor keeps
// the whole history and is left out of the closings that follow.
func (s *InvestorService) SetActive(ctx context.Context, actor security.Subject, id string, active bool) (*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *record); err != nil {
		return nil, err
	}
	if active {
		err = record.Reactivate(s.now())
	} else {
		err = record.Suspend(s.now())
	}
	if err != nil && active {
		return nil, validation.Errors{"status": {message("already_active")}}
	}
	if err != nil {
		return nil, validation.Errors{"status": {message("already_suspended")}}
	}
	if _, err := record.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, map[bool]string{true: EventInvestorReactivated, false: EventInvestorSuspended}[active], "investor", record.ID, nil); err != nil {
		return nil, err
	}
	return record, nil
}

// Find returns one investor for the admin, and asks the policy twice.
//
// The first call is on the empty candidate, because there is no way to read the
// record without a Grant and no way to hold a Grant without a decision. What it
// decides is whether this subject may manage investors at all -- an investor
// reads their own row through Mine, never by naming an identifier.
//
// The second call is on the record that came back, and it is the one a rule
// about the record itself depends on.
func (s *InvestorService) Find(ctx context.Context, actor security.Subject, id string) (*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}

	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}

	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *record); err != nil {
		return nil, err
	}
	return record, nil
}

// Mine returns the investor whose login user is the subject, which is how the
// investor's own panel finds what to show. ErrNotFound when the subject is not
// an investor of this tenant.
func (s *InvestorService) Mine(ctx context.Context, actor security.Subject) (*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := Investors(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *record); err != nil {
		return nil, err
	}
	return record, nil
}

// List returns a page of investors, for the admin.
//
// It authorizes once, on the empty candidate, and the tenant filter in the
// statement is what bounds the rows. Listing every investor is an admin's
// question: the probe for InvestorsManage refuses an investor, who reads their
// own row through Mine instead.
func (s *InvestorService) List(ctx context.Context, actor security.Subject, q data.Query) ([]*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}

	column, ok := sortableInvestor[q.Sort]
	if !ok {
		return nil, fmt.Errorf("investor: sort field not allowed: %q", q.Sort)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}

	page := Investors(s.db)
	if q.Cursor != "" {
		anchor, err := Investors(s.db).WhereKey(q.Cursor).Value(ctx, g, column)
		if err != nil {
			return nil, err
		}
		if anchor == nil {
			return nil, nil
		}
		page = page.Where(func(after *InvestorQuery) {
			after.Where(column, ">", anchor).
				OrWhere(func(equal *InvestorQuery) {
					equal.Where(column, "=", anchor).Where("id", ">", q.Cursor)
				})
		})
	}

	return page.OrderBy(column).OrderBy("id").Limit(limit).Get(ctx, g)
}
