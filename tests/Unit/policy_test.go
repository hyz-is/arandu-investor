package unit_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/filesystem"

	investor "github.com/hyz-is/arandu-investor"
	wallet "github.com/hyz-is/arandu-wallet"
)

// The four properties this package exists to keep are checked here, and they
// are checked against the code rather than described in a document:
//
//  1. the policy denies every action, and has no branch that allows one;
//  2. the service authorizes before constructing or running the generated query;
//  3. the tenant comes from the Grant;
//  4. nothing reaches the database without passing through the first two.
//
// The fourth is checked by the handle these tests pass in. It wraps a nil
// *sql.DB, so any statement that were issued would panic and fail the test
// loudly -- which makes "the refusal happened before the Model" a fact the
// suite proves rather than a comment. The structural twin in audit_test.go
// keeps that order visible on every service method, including an allowed path.

// everyAction is the whole set the policy answers about. A test that listed
// five of six would pass while the sixth was open.
var everyAction = []security.Action{
	investor.InvestorsManage,
	investor.EntriesManage,
	investor.CostsManage,
	investor.DashboardView,
	investor.ContractView,
	investor.ReportsView,
}

// manageActions are the ones only the admin takes.
var manageActions = []security.Action{investor.InvestorsManage, investor.EntriesManage, investor.CostsManage}

// readActions are the ones an investor takes about their own row.
var readActions = []security.Action{investor.DashboardView, investor.ContractView, investor.ReportsView}

// signedIn is a person with a session and no role this package knows: the
// subject every refusal below is written for.
func signedIn() security.Subject {
	return security.Subject{ID: "user-1", Tenant: "acme", Roles: []string{"admin"}, Verified: true}
}

// finance is the subject the application gave the package's admin role.
func finance() security.Subject {
	return security.Subject{ID: "user-9", Tenant: "acme", Roles: []string{investor.AdminRole}, Verified: true}
}

func row(id, tenant, user string) investor.Investor {
	return investor.Investor{ID: id, TenantID: tenant, UserID: user, Name: "Ana", Kind: investor.Person}
}

func TestThePolicyRefusesWhatNoRuleAllows(t *testing.T) {
	t.Parallel()

	// A role named "admin" is the application's, not this package's: it opens
	// nothing here. The package's own role is AdminRole, and nothing else.
	for _, action := range manageActions {
		_, err := security.Authorize(context.Background(), investor.InvestorPolicy{},
			signedIn(), action, investor.Investor{})
		if !errors.Is(err, security.ErrForbidden) {
			t.Errorf("a subject without %s was allowed %s: got %v, want ErrForbidden", investor.AdminRole, action, err)
		}
	}
	for _, action := range readActions {
		_, err := security.Authorize(context.Background(), investor.InvestorPolicy{},
			signedIn(), action, row("inv-2", "acme", "somebody-else"))
		if !errors.Is(err, security.ErrForbidden) {
			t.Errorf("a subject read %s of somebody else's investor: got %v, want ErrForbidden", action, err)
		}
	}
}

func TestTheInvestorReadsTheirOwnRowAndNothingElse(t *testing.T) {
	t.Parallel()

	mine := row("inv-1", "acme", "user-1")
	for _, action := range readActions {
		if _, err := security.Authorize(context.Background(), investor.InvestorPolicy{}, signedIn(), action, mine); err != nil {
			t.Errorf("the investor was refused %s on their own row: %v", action, err)
		}
	}
	for _, action := range manageActions {
		if _, err := security.Authorize(context.Background(), investor.InvestorPolicy{}, signedIn(), action, mine); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("the investor was allowed %s on their own row: %v", action, err)
		}
	}
}

func TestTheAdminActsAcrossItsTenantAndNoFurther(t *testing.T) {
	t.Parallel()

	for _, action := range everyAction {
		if _, err := security.Authorize(context.Background(), investor.InvestorPolicy{}, finance(), action, investor.Investor{}); err != nil {
			t.Errorf("the admin was refused %s: %v", action, err)
		}
		if _, err := security.Authorize(context.Background(), investor.InvestorPolicy{}, finance(), action, row("inv-1", "acme", "user-1")); err != nil {
			t.Errorf("the admin was refused %s on an investor of its tenant: %v", action, err)
		}
		if _, err := security.Authorize(context.Background(), investor.InvestorPolicy{}, finance(), action, row("inv-1", "globex", "user-1")); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("the admin was allowed %s on another tenant's investor: %v", action, err)
		}
	}
}

func TestThePolicyDeniesARecordOfAnotherTenant(t *testing.T) {
	t.Parallel()

	other := row("record-1", "globex", "user-1")

	err := investor.InvestorPolicy{}.Can(context.Background(), signedIn(), investor.DashboardView, other)
	if err == nil {
		t.Fatal("the policy allowed a record belonging to another tenant")
	}
	// The message is asserted because the tenant check is the one refusal that
	// has to survive somebody opening the actions below it.
	if !strings.Contains(err.Error(), "another tenant") {
		t.Fatalf("the refusal did not name the tenant: %v", err)
	}
}

func TestThePolicyDeniesAGuest(t *testing.T) {
	t.Parallel()

	for _, action := range everyAction {
		_, err := security.Authorize(context.Background(), investor.InvestorPolicy{},
			security.Guest("acme"), action, investor.Investor{})
		if !errors.Is(err, security.ErrForbidden) {
			t.Fatalf("a guest was allowed %s: got %v, want ErrForbidden", action, err)
		}
	}
}

func TestAuthorizeRefusesASubjectThatIsNobody(t *testing.T) {
	t.Parallel()

	// The zero Subject is a session that failed to load, not an anonymous
	// reader, and it is refused before the policy is consulted. A package that
	// answered it as a guest would answer a broken session as a visitor.
	_, err := security.Authorize(context.Background(), investor.InvestorPolicy{},
		security.Subject{}, investor.DashboardView, investor.Investor{})
	if !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("an empty subject was authorized: got %v, want ErrForbidden", err)
	}
}

// nilHandle is a handle over no database.
//
// Any statement issued through it panics, which is what makes these tests
// prove that the refusal came first: a service that reached the Model before
// authorizing would crash here rather than pass.
func nilHandle() *data.DB { return data.Wrap(nil, data.DialectSQLite) }

func TestTheServiceRefusesBeforeReachingTheModel(t *testing.T) {
	t.Parallel()

	// A nil handle makes even construction of Investors panic, at the
	// GetQueryGrammar the query asks the handle for. This catches moving the
	// generated query constructor -- not only its terminal -- ahead of
	// authorization.
	service := investor.NewInvestorService(nil)
	ctx := context.Background()
	guest := security.Guest("acme")

	if _, err := service.Find(ctx, signedIn(), "record-1"); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Find reached the Model before the policy refusal: %v", err)
	}
	if _, err := service.Mine(ctx, guest); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Mine reached the Model before the policy refusal: %v", err)
	}
	if _, err := service.List(ctx, signedIn(), data.Query{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("List reached the Model before the policy refusal: %v", err)
	}
	if _, err := service.Create(ctx, signedIn(), validPerson()); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Create reached the Model before the policy refusal: %v", err)
	}
	if _, err := service.Update(ctx, signedIn(), "record-1", investor.UpdateRequest{Name: "Ana", Email: "ana@example.com"}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Update reached the Model before the policy refusal: %v", err)
	}
	if _, err := service.SetActive(ctx, signedIn(), "record-1", false); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("SetActive reached the Model before the policy refusal: %v", err)
	}
}

// validPerson is a request every check passes: a well-formed CPF, a name and
// an e-mail. Tests that want a refusal change one field of it.
func validPerson() investor.CreateRequest {
	return investor.CreateRequest{
		UserID: "user-1", Kind: investor.Person, Document: "529.982.247-25",
		Name: "Ana", Email: "ana@example.com",
	}
}

// recordingHandle runs nothing and keeps every statement it is handed, so a
// test reads what the table compiles instead of reading its settings. The table
// keeps its settings to itself; what it writes is the behaviour they decide.
type recordingHandle struct {
	*data.DB
	statements []recordedStatement
}

type recordedStatement struct {
	sql      string
	bindings []any
}

// newRecordingHandle borrows the grammar and the processor of an SQLite handle
// over no database, and answers every statement itself.
func newRecordingHandle() *recordingHandle { return &recordingHandle{DB: nilHandle()} }

func (h *recordingHandle) record(sql string, bindings []any) {
	h.statements = append(h.statements, recordedStatement{sql: sql, bindings: slices.Clone(bindings)})
}

func (h *recordingHandle) Select(_ context.Context, sql string, bindings []any, _ bool) ([]query.Record, error) {
	h.record(sql, bindings)
	return nil, nil
}

func (h *recordingHandle) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

func (h *recordingHandle) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

// first is the first recorded statement that starts with verb.
func (h *recordingHandle) first(t *testing.T, verb string) recordedStatement {
	t.Helper()
	for _, statement := range h.statements {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(statement.sql)), verb) {
			return statement
		}
	}
	t.Fatalf("no %s statement was issued; recorded %v", verb, h.statements)
	return recordedStatement{}
}

// TestInvestorsReturnsAWiredTenantScopedQuery holds what investorTable declares,
// through what it makes the generated query do: the table it reads, a key the
// application writes and the engine never fills, and a tenant_id taken from the
// Grant on the way in and on the way out.
func TestInvestorsReturnsAWiredTenantScopedQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handle := newRecordingHandle()
	table := investor.Investors(handle).Base().Table()
	if table.Name() != "investors" {
		t.Fatalf("Investors table = %q, want investors", table.Name())
	}
	if key := table.MorphModel(handle); key.GetKeyName() != "id" || key.GetKeyType() != "string" {
		t.Fatalf("Investors key is %q of type %q; want id, as text", key.GetKeyName(), key.GetKeyType())
	}

	row, err := investor.Investors(handle).New()
	if err != nil {
		t.Fatalf("building a row: %v", err)
	}
	if row.Table() != table {
		t.Fatal("Investors returned an entity whose embedded Model is not wired to its table")
	}

	// The key is application-generated text: the insert carries the one the row
	// was given, and the engine is never asked for one. An incrementing key
	// would go through the processor, which runs on no database here, and a key
	// the model generated would replace the one the service wrote.
	row.ID = "record-1"
	if _, err := row.Save(ctx, security.SystemGrant(investor.InvestorsManage, "acme")); err != nil {
		t.Fatalf("saving through the recording handle: %v", err)
	}
	insert := handle.first(t, "insert")
	if !slices.Contains(insert.bindings, any("record-1")) || row.ID != "record-1" {
		t.Fatalf("the insert %q %v does not write the application key, or the row lost it (%q)", insert.sql, insert.bindings, row.ID)
	}
	if !strings.Contains(insert.sql, "tenant_id") || !slices.Contains(insert.bindings, any("acme")) {
		t.Fatalf("the insert %q %v does not stamp tenant_id with the Grant's tenant", insert.sql, insert.bindings)
	}

	// And the tenant column scopes a read by the Grant's tenant.
	if _, err := investor.Investors(handle).WhereKey("record-1").First(ctx, security.SystemGrant(investor.DashboardView, "acme")); err != nil {
		t.Fatalf("reading through the recording handle: %v", err)
	}
	read := handle.first(t, "select")
	if !strings.Contains(read.sql, "tenant_id") || !slices.Contains(read.bindings, any("acme")) {
		t.Fatalf("the read %q %v is not filtered by tenant_id with the Grant's tenant", read.sql, read.bindings)
	}
}

// TestTheMissingRecordIsTheModelsNotFound holds the one link between this
// package's sentinel and the router's answer. The router answers
// model.ErrModelNotFound with 404 and has never heard of ErrNotFound, so the
// sentinel is a 404 only while it wraps the model's.
func TestTheMissingRecordIsTheModelsNotFound(t *testing.T) {
	t.Parallel()

	if !errors.Is(investor.ErrNotFound, model.ErrModelNotFound) {
		t.Fatal("ErrNotFound does not wrap model.ErrModelNotFound, so the router would answer a missing record with 500")
	}
}

func TestASystemGrantWithoutATenantReachesNothing(t *testing.T) {
	t.Parallel()

	// A system grant with no tenant names no customer. The Model refuses it
	// while preparing the query, before the nil handle can issue a statement.
	_, err := investor.Investors(nilHandle()).WhereKey("record-1").First(
		context.Background(), security.SystemGrant(investor.DashboardView, ""))
	if !errors.Is(err, model.ErrNoTenant) {
		t.Fatalf("a system grant with no tenant returned %v, want ErrNoTenant", err)
	}
}

func TestTheTenantComesFromTheGrant(t *testing.T) {
	t.Parallel()

	g := security.SystemGrant(investor.DashboardView, "acme")
	if got := data.Tenant(g); got != "acme" {
		t.Fatalf("data.Tenant(g) = %q, want %q", got, "acme")
	}

	// And a Grant nobody issued carries no tenant at all, so a statement that
	// took its tenant from anywhere else would be reading rows this Grant does
	// not name.
	if got := data.Tenant(security.Grant{}); got != "" {
		t.Fatalf("the zero Grant carries the tenant %q, want none", got)
	}
}

func TestTheRequestValidatesItsInput(t *testing.T) {
	t.Parallel()

	if errs := (investor.CreateRequest{}).Validate(); !errs.Any() {
		t.Fatal("an empty request validated")
	}
	if errs := validPerson().Validate(); errs.Any() {
		t.Fatalf("a valid person was rejected: %v", errs)
	}

	refused := map[string]func(*investor.CreateRequest){
		"a CPF with a wrong check digit": func(r *investor.CreateRequest) { r.Document = "529.982.247-24" },
		"a CNPJ for a person":            func(r *investor.CreateRequest) { r.Document = "11.222.333/0001-81" },
		"a kind nobody declared":         func(r *investor.CreateRequest) { r.Kind = "trust" },
		"a name past the maximum":        func(r *investor.CreateRequest) { r.Name = strings.Repeat("a", 161) },
		"no e-mail":                      func(r *investor.CreateRequest) { r.Email = "" },
		"no login user":                  func(r *investor.CreateRequest) { r.UserID = "" },
	}
	for name, change := range refused {
		request := validPerson()
		change(&request)
		if errs := request.Validate(); !errs.Any() {
			t.Errorf("%s validated", name)
		}
	}

	company := validPerson()
	company.Kind = investor.Company
	company.Document = "11.222.333/0001-81"
	if errs := company.Validate(); !errs.Has("legal_name", "legal_representative") {
		t.Errorf("a company without its registered name and its representative validated: %v", errs)
	}
	company.LegalName, company.LegalRepresentative = "Ana Investimentos Ltda", "Ana"
	if errs := company.Validate(); errs.Any() {
		t.Errorf("a valid company was rejected: %v", errs)
	}
}

func TestTheConfigurationRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	disk, wallets := testDisk(t), testWallet(t, nilHandle())
	for name, cfg := range map[string]investor.Config{
		"no Disk for the contracts": {Wallet: wallets},
		"no arandu-wallet":          {Files: disk},
		"relative prefix":           {Prefix: "investor", Files: disk, Wallet: wallets},
		"page size too big":         {PageSize: investor.MaxPageSize + 1, Files: disk, Wallet: wallets},
		"negative page size":        {PageSize: -1, Files: disk, Wallet: wallets},
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("the configuration with %s was accepted", name)
		}
	}

	// The Disk and the wallet are the settings an application has to write. Everything else
	// has a default, and there is no tenant to name: every tenant comes from the
	// Grant.
	if err := (investor.Config{Files: disk, Wallet: wallets}).Validate(); err != nil {
		t.Fatalf("a configuration with only the Disk and the wallet was refused: %v", err)
	}
}

// testDisk is a Disk over a fresh directory, the application's Disk in small.
func testDisk(t testing.TB) *filesystem.Disk {
	t.Helper()
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("building a local Disk: %v", err)
	}
	return filesystem.NewDisk("contracts", adapter)
}

// testWallet is arandu-wallet over the same handle, the one instance an
// application passes in Config.Wallet.
func testWallet(t testing.TB, db *data.DB) *wallet.WalletService {
	t.Helper()
	sessions := security.NewSessionStore([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false, security.NewMemoryBackend())
	module, err := wallet.New(wallet.Config{Tenant: "acme"}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	return module.Service()
}
