// Package investor is an Arandu module: one entity with its generated query, one
// policy that decides about it, one service that orchestrates each use case, and
// the routes that reach them.
//
// The files are laid out by role rather than by layer, so the whole package
// reads top to bottom:
//
//	module.go      -> registration, routes, handlers and migrations
//	config.go      -> what the application passes in
//	model.go       -> the entity, and what it may answer with
//	policy.go      -> who may do what
//	service.go     -> the use cases, and Model access after authorization
//	views.go       -> the files the application takes ownership of
//
// An application registers it explicitly. There is no service provider, no
// container and no discovery: the wiring is three lines somebody wrote, and
// reading them is how they learn what the application is made of.
package investor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/translation"
	hview "github.com/arandu-io/hesape/view"
	wallet "github.com/hyz-is/arandu-wallet"
)

// Module is what the application registers.
//
// It implements foundation.Module, which is Name and Routes and nothing else --
// that pair is the whole public contract between a package and the framework.
//
// It also implements foundation.Migratable, because it owns a table, and
// foundation.Publishable, because it hands view sources to the project. The
// other optional interfaces are declared beside Module in the framework and are
// opted into the same way, by implementing them: Bootable to prepare state at
// boot, Background to run a loop of its own, Schedulable to declare work for
// the scheduler, Health to report on the storage it depends on, Closable to
// give resources back at shutdown.
type Module struct {
	cfg      Config
	svc      *InvestorService
	sessions *security.SessionStore
}

// Compile-time proof that the module honors the contracts it claims.
var (
	_ foundation.Module      = (*Module)(nil)
	_ foundation.Migratable  = (*Module)(nil)
	_ foundation.Bootable    = (*Module)(nil)
	_ foundation.Publishable = (*Module)(nil)
)

// New returns the module, or the reason it cannot be built.
//
// The collaborators are parameters and not fields somebody fills in afterwards:
// a module that could be registered half-wired is a module whose first request
// is the thing that reports the missing half.
//
// It returns an error rather than panicking or carrying on, because everything
// it refuses is a wiring mistake, and a wiring mistake found at boot costs one
// restart. The same mistake found later is a request that reached a nil handle.
func New(cfg Config, db *data.DB, sessions *security.SessionStore) (*Module, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("investor: New needs a database handle: this package owns a table, and there is no in-memory mode that would let it start without one")
	}
	if sessions == nil {
		return nil, errors.New("investor: New needs a session store: every route is mounted behind RequireAuth, which reads it to admit a request and to put who is asking on it")
	}
	cfg = cfg.withDefaults()
	svc := NewInvestorService(db)
	svc.files = cfg.Files
	svc.loc = cfg.Location
	svc.wallets = cfg.Wallet
	svc.payer = cfg.Payer
	svc.activity = cfg.Activity
	m := &Module{
		cfg:      cfg,
		svc:      svc,
		sessions: sessions,
	}
	labels := m.Labels(FallbackLocale)
	svc.describe = func(event string) string { return labels.T("activity." + strings.ReplaceAll(event, ".", "_")) }
	return m, nil
}

// Service is the use cases, for an application that calls them from its own
// code: the events endpoint it owns, a job, a command. Every method asks the
// policy, so handing it out opens nothing.
func (m *Module) Service() *InvestorService { return m.svc }

// Name is the module identifier: a lowercase slug, stable, no spaces.
//
// It is what `aru route:list` groups by and what the route names are prefixed with,
// so changing it changes addresses that other code has already written down.
func (m *Module) Name() string { return "investor" }

// Routes registers every route of the module: the investor's and the admin's.
//
// They are named, so a URL is built from a name rather than written out a
// second time somewhere else -- two spellings of one address disagree, and the
// failure when they do is a link to a 404.
//
// Every route needs somebody signed in, so the guard is mounted once, on the
// router they are registered on, rather than route by route: r is replaced by
// the guarded group before the first registration, and no unguarded router is
// left in scope for the next route to be added to. RequireAuth sends a request
// with no session to the sign-in screen, and puts the subject of one that has a
// session on its context, where a handler reads it with ctx.User;
// Config.Middleware runs after it, and Config.AdminMiddleware after that on
// the admin's routes.
func (m *Module) Routes(r *fhttp.Router) {
	m.InvestorRoutes(r)
	m.AdminRoutes(r)
}

// InvestorRoutes registers the investor's own routes alone, under Prefix, for
// an application that mounts them inside its own side -- a customer's panel --
// with that side's guards on r.
func (m *Module) InvestorRoutes(r *fhttp.Router) { m.register(r, false) }

// AdminRoutes registers the admin's routes alone, under AdminPrefix, for an
// application that mounts them inside its operator console with that
// console's guards on r.
func (m *Module) AdminRoutes(r *fhttp.Router) { m.register(r, true) }

func (m *Module) register(r *fhttp.Router, admin bool) {
	guards := []fhttp.Middleware{middleware.RequireAuth(m.sessions)}
	for _, mw := range m.cfg.Middleware {
		guards = append(guards, mw)
	}
	if admin {
		for _, mw := range m.cfg.AdminMiddleware {
			guards = append(guards, mw)
		}
	}
	r = r.Group("", guards...)
	handlers := m.handlers()
	for _, rt := range routeTable(m.cfg) {
		if rt.admin != admin {
			continue
		}
		r.Action(rt.method, rt.path, handlers[rt.name]).Name(rt.name)
	}
}

// handlers are the handlers of the route table, by route name.
func (m *Module) handlers() map[string]func(*fhttp.Context) error {
	return map[string]func(*fhttp.Context) error{
		"investor.me":                         m.mine,
		"investor.me.contracts":               m.myContracts,
		"investor.contracts.file":             m.contractFile,
		"investor.contracts.statement":        m.statement,
		"investor.contracts.projection":       m.projection,
		"investor.costs.index":                m.costs,
		"investor.me.invoices":                m.myInvoices,
		"investor.invoices.withdraw":          m.withdrawInvoice,
		"investor.me.reports.monthly":         m.myMonthlyReport,
		"investor.me.reports.annual":          m.myAnnualReport,
		"investor.me.payout_accounts.store":   m.storePayoutAccount,
		"investor.invoices.receipt":           m.invoiceReceipt,
		"investor.admin.investors.index":      m.index,
		"investor.admin.investors.store":      m.store,
		"investor.admin.investors.show":       m.show,
		"investor.admin.investors.update":     m.update,
		"investor.admin.investors.suspend":    m.suspend,
		"investor.admin.investors.reactivate": m.reactivate,
		"investor.admin.contracts.index":      m.contracts,
		"investor.admin.contracts.store":      m.storeContract,
		"investor.admin.contracts.amend":      m.amendContract,
		"investor.admin.entries.index":        m.entries,
		"investor.admin.entries.store":        m.storeEntry,
		"investor.admin.entries.reverse":      m.reverseEntry,
		"investor.admin.costs.index":          m.adminCosts,
		"investor.admin.costs.store":          m.storeCost,
		"investor.admin.costs.end":            m.endCost,
		"investor.admin.closings.store":       m.closeContract,
		"investor.admin.closings.due":         m.closeDue,
		"investor.admin.invoices.index":       m.invoices,
		"investor.admin.reports.monthly":      m.monthlyReport,
		"investor.admin.reports.annual":       m.annualReport,
		"investor.admin.withdrawals.index":    m.withdrawals,
		"investor.admin.withdrawals.process":  m.startWithdrawal,
		"investor.admin.withdrawals.pay":      m.payWithdrawal,
		"investor.admin.withdrawals.reject":   m.rejectWithdrawal,
	}
}

// screen is what every page of this package is drawn with: the page hesape's
// view.New builds, the application's chrome from Config.Chrome, the sentences
// in the language the request negotiated, and where the package is mounted.
func (m *Module) screen(ctx *fhttp.Context, who security.Subject, titleKey string, admin bool) (Screen, error) {
	labels := m.Labels(translation.Locale(ctx.Ctx()))
	page := hview.New(ctx, labels.T(titleKey))
	page.Authenticated = who.ID != ""
	sc := Screen{Page: page, Labels: labels, Prefix: m.cfg.Prefix, URLs: m.URLs()}
	if m.cfg.Chrome != nil {
		chrome, err := m.cfg.Chrome(ctx, ChromeRequest{Title: page.Title, Admin: admin, Subject: who, Page: page})
		if err != nil {
			return Screen{}, err
		}
		sc.chrome = chrome
	}
	return sc, nil
}

// today is the day it is in the application's timezone.
func (m *Module) today() Day { return DayOf(time.Now(), m.cfg.Location) }

// PublishCommand is what an application runs to take ownership of the views
// this package offers.
//
// It is spelled out as a constant so that whatever says it -- the refusal
// below, or a message an application writes for its own operators -- says one
// thing. A person who is told two different commands for one job tries both.
//
// The command reads the modules the application registered and writes what each
// one declares, which is why it is the application's command and not this
// package's: nothing outside the application knows which modules it holds.
const PublishCommand = "aru vendor:publish --apply"

// Boot refuses to serve when a view this package renders is not in the binary.
//
// A compiled view registers itself from init(), so by the time anything boots
// the question has one answer already: either the application published the
// files, compiled them and imported the package they became, or it did not.
// Asking here turns "did anybody run the install command" into one refusal at
// start-up that names the views and the command, instead of a 500 on the first
// request that reached one of them -- which is where it used to be answered,
// once per page, to whoever happened to open it.
//
// It also holds the destination. Every file the archive offers has to land
// under the module directory named after this module: an archive that reached
// resources/views/home.kyse.go would land on a page the application wrote, and
// what publishes the files writes what the archive says.
func (m *Module) Boot(context.Context) error {
	prefix := viewRoot + "/" + moduleDir + "/" + m.Name() + "/"
	var stray []string
	for _, path := range PublishedPaths() {
		if !strings.HasPrefix(path, prefix) {
			stray = append(stray, path)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("investor: %s would be published outside %s, where it lands on a file the application wrote",
			strings.Join(stray, ", "), prefix)
	}

	registered := make(map[string]bool)
	for _, name := range hview.Registered() {
		registered[name] = true
	}
	var missing []string
	for _, name := range ViewNames() {
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// The compiled packages are named because the import is the half of the
		// install nothing else can do: the command writes the sources, the view
		// compiler turns them into Go, and a package the application does not
		// import is not linked at all.
		imports := make([]string, 0, len(ViewPackages()))
		for _, pkg := range ViewPackages() {
			imports = append(imports, "<module path>/"+pkg)
		}
		return fmt.Errorf("investor: no view is registered as %s. Run `%s`, then `aru view:build`, then import %s in bootstrap/app.go",
			strings.Join(missing, ", "), PublishCommand, strings.Join(imports, ", "))
	}
	return nil
}

// Handlers are thin on purpose: read the input, ask the service, answer. No
// rule, database handle or Model construction lives here. A handler that
// reached data directly would skip the service's policy boundary, and the
// layout makes that visible rather than relying on review.
//
// Who is asking comes from ctx.User, which reads the subject RequireAuth put on
// the request. No handler loads the session again or takes an identity from the
// input. The second value ctx.User answers is not consulted: behind the guard
// there is always a subject, and were there none, the zero subject it answers
// would be refused by security.Authorize before any policy ran.
//
// An error from the service is returned as it is, and the router answers it:
// validation.Errors goes back where the request came from with the messages in
// the flash, ErrNotFound is 404 because it wraps model.ErrModelNotFound, and a
// policy refusal is 403. The answer is the status and its standard sentence,
// never the error's own text -- telling a client why a policy said no tells it
// what exists, one request at a time, and the reason belongs in the log. Any
// other error is one nobody expected, and it reaches the error page rather than
// a 200 with an empty body.

// index answers a page of records.
func (m *Module) index(ctx *fhttp.Context) error {
	query := data.Query{
		Sort:   ctx.Query("sort"),
		Cursor: ctx.Query("cursor"),
		Limit:  m.cfg.PageSize,
	}

	who, _ := ctx.User()
	records, err := m.svc.List(ctx.Ctx(), who, query)
	if err != nil {
		return err
	}

	// A full page is the only one that can have a successor. A short page is
	// the last one, and offering a cursor for it would be offering a next page
	// that comes back empty.
	cursor := ""
	if len(records) == m.cfg.PageSize {
		cursor = records[len(records)-1].ID
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, collectionFromPointers(records, cursor))
	}
	sc, err := m.screen(ctx, who, "screen.admin_investors_title", true)
	if err != nil {
		return err
	}
	rows := make([]InvestorRow, 0, len(records))
	for _, r := range records {
		rows = append(rows, investorRow(m.URLs(), sc.Labels, r))
	}
	return ctx.View(ViewAdminInvestors, AdminInvestorsPageData{
		Screen: sc, Rows: rows, StoreURL: m.URLs().Investors(),
		Next: cursor, NextURL: nextPageURL(m.URLs().Investors(), cursor),
	})
}

// nextPageURL is the address of the page after the one shown, empty on the
// last.
func nextPageURL(base, cursor string) string {
	if cursor == "" {
		return ""
	}
	return base + "?cursor=" + url.QueryEscape(cursor)
}

// show answers one record.
func (m *Module) show(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.svc.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, resourceFromPointer(record))
	}
	contracts, err := m.svc.InvestorContracts(ctx.Ctx(), who, record.ID)
	if err != nil {
		return err
	}
	invoices, err := m.svc.InvoiceViews(ctx.Ctx(), who, record.ID)
	if err != nil {
		return err
	}
	sc, err := m.screen(ctx, who, "screen.admin_investor_title", true)
	if err != nil {
		return err
	}
	now := time.Now().In(m.cfg.Location)
	u := m.URLs()
	return ctx.View(ViewAdminInvestor, AdminInvestorPageData{
		Screen:              sc,
		Investor:            investorRow(m.URLs(), sc.Labels, record),
		LegalName:           record.LegalName,
		LegalRepresentative: record.LegalRepresentative,
		Contracts:           contractRows(m.URLs(), sc.Labels, contracts),
		Invoices:            invoiceRows(m.URLs(), sc.Labels, invoices),
		ContractStoreURL:    u.InvestorContracts(record.ID),
		UpdateURL:           u.Investor(record.ID),
		SuspensionURL:       u.Suspension(record.ID),
		ReportsURL:          u.InvestorReports(record.ID),
		Year:                now.Year(),
		Month:               int(now.Month()),
		Today:               string(m.today()),
	})
}

// mine answers the investor whose login user is asking.
func (m *Module) mine(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, resourceFromPointer(record))
	}
	contracts, err := m.svc.InvestorContracts(ctx.Ctx(), who, record.ID)
	if err != nil {
		return err
	}
	invoices, err := m.svc.InvoiceViews(ctx.Ctx(), who, record.ID)
	if err != nil {
		return err
	}
	accounts, err := m.svc.PayoutAccounts(ctx.Ctx(), who, record.ID)
	if err != nil {
		return err
	}
	sc, err := m.screen(ctx, who, "screen.dashboard_title", false)
	if err != nil {
		return err
	}
	now := time.Now().In(m.cfg.Location)
	available, held, year, withdrawn := invoiceTotals(invoices, now.Year())
	row := investorRow(m.URLs(), sc.Labels, record)
	return ctx.View(ViewDashboard, DashboardPageData{
		Screen:            sc,
		Investor:          row.Name,
		Document:          row.Document,
		Available:         brl(available),
		Held:              brl(held),
		ReceivedYear:      brl(year),
		WithdrawnTotal:    brl(withdrawn),
		Contracts:         contractRows(m.URLs(), sc.Labels, contracts),
		Invoices:          invoiceRows(m.URLs(), sc.Labels, invoices),
		Accounts:          accountOptions(accounts),
		Year:              now.Year(),
		Month:             int(now.Month()),
		ReportsURL:        m.URLs().MyReports(),
		PixURL:            m.URLs().PayoutAccounts(),
		PayoutAccountsURL: m.URLs().PayoutAccounts(),
	})
}

// store registers one investor.
func (m *Module) store(ctx *fhttp.Context) error {
	in := CreateRequest{
		UserID:              ctx.Input("user_id"),
		Kind:                PersonKind(ctx.Input("person_kind")),
		Document:            ctx.Input("document"),
		Name:                ctx.Input("name"),
		LegalName:           ctx.Input("legal_name"),
		LegalRepresentative: ctx.Input("legal_representative"),
		Email:               ctx.Input("email"),
	}

	who, _ := ctx.User()
	record, err := m.svc.Create(ctx.Ctx(), who, in)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.ID))
	}
	return ctx.JSON(stdhttp.StatusCreated, resourceFromPointer(record))
}

// update changes the names and the e-mail of one investor.
func (m *Module) update(ctx *fhttp.Context) error {
	in := UpdateRequest{
		Name:                ctx.Input("name"),
		LegalName:           ctx.Input("legal_name"),
		LegalRepresentative: ctx.Input("legal_representative"),
		Email:               ctx.Input("email"),
	}
	who, _ := ctx.User()
	record, err := m.svc.Update(ctx.Ctx(), who, ctx.Param("id"), in)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.ID))
	}
	return ctx.JSON(stdhttp.StatusOK, resourceFromPointer(record))
}

// suspend leaves one investor out of the closings that follow.
func (m *Module) suspend(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.svc.SetActive(ctx.Ctx(), who, ctx.Param("id"), false)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.ID))
	}
	return ctx.JSON(stdhttp.StatusOK, resourceFromPointer(record))
}

// reactivate brings one suspended investor back.
func (m *Module) reactivate(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.svc.SetActive(ctx.Ctx(), who, ctx.Param("id"), true)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.ID))
	}
	return ctx.JSON(stdhttp.StatusOK, resourceFromPointer(record))
}

// contractForm reads a contract or an amendment out of a multipart form: the
// terms as fields, the signed copy as the part named "file". The body is read
// up to the limit the service enforces, plus room for the fields.
func contractForm(ctx *fhttp.Context) (ContractRequest, io.ReadCloser, error) {
	ctx.Request.Body = stdhttp.MaxBytesReader(ctx.Response, ctx.Request.Body, MaxContractBytes+(1<<20))
	if err := ctx.Request.ParseMultipartForm(MaxContractBytes); err != nil {
		return ContractRequest{}, nil, validation.Errors{"file": {"could not be read; send the form as multipart/form-data, under the size limit"}}
	}
	share, _ := strconv.ParseInt(ctx.Request.FormValue("share_bps"), 10, 64)
	// A form types the share as a percentage, "15" or "12,5": two decimals of
	// a percentage are basis points, the same arithmetic as reais in centavos.
	if raw := ctx.Request.FormValue("share_percent"); raw != "" {
		share, _ = parseReais(raw)
	}
	day, _ := strconv.Atoi(ctx.Request.FormValue("billing_day"))
	in := ContractRequest{
		Number:      ctx.Request.FormValue("number"),
		SignedOn:    ctx.Request.FormValue("signed_on"),
		StartsOn:    ctx.Request.FormValue("starts_on"),
		EndsOn:      ctx.Request.FormValue("ends_on"),
		ShareBps:    share,
		BillingDay:  day,
		AllProducts: ctx.Request.FormValue("all_products") == "1" || ctx.Request.FormValue("all_products") == "true",
		Products:    splitProducts(ctx.Request.MultipartForm.Value["products"]),
	}
	// A form ticks "the whole application" by default; naming products is
	// what narrows it.
	if len(in.Products) > 0 {
		in.AllProducts = false
	}
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		return in, nil, nil
	}
	in.FileName = header.Filename
	in.FileType = header.Header.Get("Content-Type")
	return in, file, nil
}

// splitProducts reads product identifiers sent as repeated fields, as a
// comma-separated line, or both.
func splitProducts(values []string) []string {
	var out []string
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// contracts answers every version of every contract of one investor.
func (m *Module) contracts(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	items, err := m.svc.InvestorContracts(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(stdhttp.StatusOK, ContractList{items: items})
}

// myContracts answers the contracts of the investor who is asking.
func (m *Module) myContracts(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	me, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	items, err := m.svc.InvestorContracts(ctx.Ctx(), who, me.ID)
	if err != nil {
		return err
	}
	return ctx.JSON(stdhttp.StatusOK, ContractList{items: items})
}

// storeContract registers the original version of a contract.
func (m *Module) storeContract(ctx *fhttp.Context) error {
	in, file, err := contractForm(ctx)
	if err != nil {
		return err
	}
	if file != nil {
		defer file.Close()
	}
	who, _ := ctx.User()
	record, err := m.svc.CreateContract(ctx.Ctx(), who, ctx.Param("id"), in, file)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.InvestorID))
	}
	return ctx.JSON(stdhttp.StatusCreated, NewContractResource(*record, cleanProducts(in.Products)))
}

// amendContract registers a new version of a contract.
func (m *Module) amendContract(ctx *fhttp.Context) error {
	in, file, err := contractForm(ctx)
	if err != nil {
		return err
	}
	if file != nil {
		defer file.Close()
	}
	who, _ := ctx.User()
	record, err := m.svc.AmendContract(ctx.Ctx(), who, ctx.Param("id"), in, file)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(record.InvestorID))
	}
	return ctx.JSON(stdhttp.StatusCreated, NewContractResource(*record, cleanProducts(in.Products)))
}

// contractFile streams the signed copy of one contract version, as a download.
func (m *Module) contractFile(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, file, err := m.svc.ContractFile(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	defer file.Body.Close()
	header := ctx.Response.Header()
	header.Set("Content-Type", record.FileType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.FileName}))
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	ctx.Response.WriteHeader(stdhttp.StatusOK)
	_, err = io.Copy(ctx.Response, file.Body)
	return err
}

// statement answers a contract's statement, filtered by ?from=&to=&kind=&product=.
func (m *Module) statement(ctx *fhttp.Context) error {
	from, errFrom := ParseDay(ctx.Query("from"))
	to, errTo := ParseDay(ctx.Query("to"))
	if errFrom != nil || errTo != nil {
		return validation.Errors{"period": {"from and to must be days in the form " + DayLayout}}
	}
	who, _ := ctx.User()
	st, err := m.svc.StatementOf(ctx.Ctx(), who, ctx.Param("id"), StatementFilter{
		From: from, To: to, Kind: EntryKind(ctx.Query("kind")), ProductID: ctx.Query("product"),
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, statementResource{st})
	}
	sc, err := m.screen(ctx, who, "screen.statement_title", false)
	if err != nil {
		return err
	}
	contract := contractRows(m.URLs(), sc.Labels, []ContractResource{NewContractResource(st.Contract, nil)})[0]
	rows := statementRows(sc.Labels, st)
	estimates := false
	for _, r := range rows {
		estimates = estimates || r.Estimate
	}
	return ctx.View(ViewStatement, StatementPageData{
		Screen: sc, Contract: contract, Rows: rows,
		Gross: brl(st.Gross), Fees: brl(st.Fees), Costs: brl(st.Costs), Net: brl(st.Net),
		From: string(from), To: string(to), Kind: ctx.Query("kind"), Product: ctx.Query("product"),
		URL: contract.StatementURL, HasEstimates: estimates,
	})
}

// projection answers a contract's open period.
func (m *Module) projection(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p, err := m.svc.ProjectionOf(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, projectionResource{p})
	}
	sc, err := m.screen(ctx, who, "screen.projection_title", false)
	if err != nil {
		return err
	}
	costs := make([]ProjectedCostRow, 0, len(p.Costs))
	for _, c := range p.Costs {
		scope := sc.Labels.T("field.all_products")
		if !bool(c.Item.AllProducts) {
			scope = strings.Join(c.Products, ", ")
		}
		costs = append(costs, ProjectedCostRow{
			Name: c.Item.Name, Category: c.Item.Category, Scope: scope, Amount: brl(c.Amount),
			Recurrence: sc.Labels.T("recurrence." + string(c.Item.Recurrence)),
		})
	}
	return ctx.View(ViewProjection, ProjectionPageData{
		Screen:      sc,
		Contract:    contractRows(m.URLs(), sc.Labels, []ContractResource{NewContractResource(p.Contract, nil)})[0],
		PeriodStart: dayText(p.PeriodStart), PeriodEnd: dayText(p.PeriodEnd.AddDays(-1)),
		Costs: costs, Gross: brl(p.Gross), Fees: brl(p.Fees), ProjectedCosts: brl(p.ProjectedCosts),
		Ratio: ratioText(p.EstimatedCostRatioPPM), EstimatedNet: brl(p.EstimatedNet), EstimatedAmount: brl(p.EstimatedAmount),
	})
}

// adminCosts is the declared costs, with the form that declares one.
func (m *Module) adminCosts(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	views, err := m.svc.AdminCosts(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, costsResource(views))
	}
	sc, err := m.screen(ctx, who, "screen.admin_costs_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewAdminCosts, AdminCostsPageData{
		Screen: sc, Rows: costRows(m.URLs(), sc.Labels, views, m.today()),
		StoreURL: m.URLs().AdminCosts(), Today: string(m.today()),
	})
}

// entries is the admin's books, a page at a time, searched, filtered and
// ordered on the server.
func (m *Module) entries(ctx *fhttp.Context) error {
	page, _ := strconv.Atoi(ctx.Query("page"))
	q := EntriesQuery{
		Search: ctx.Query("q"), Kind: EntryKind(ctx.Query("kind")),
		Sort: ctx.Query("sort"), Dir: ctx.Query("dir"), Page: page, PerPage: m.cfg.PageSize,
	}
	who, _ := ctx.User()
	rows, total, err := m.svc.EntriesPage(ctx.Ctx(), who, q)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		items := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			items = append(items, entryFields(*e))
		}
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"items": items, "total": total})
	}
	if page < 1 {
		page = 1
	}
	pages := (total + m.cfg.PageSize - 1) / m.cfg.PageSize
	sc, err := m.screen(ctx, who, "screen.admin_books_title", true)
	if err != nil {
		return err
	}
	u := m.URLs()
	return ctx.View(ViewAdminEntries, AdminEntriesPageData{
		Screen: sc, Rows: entryRows(m.URLs(), sc.Labels, rows),
		Query: q.Search, Kind: string(q.Kind), Sort: q.Sort, Dir: q.Dir,
		CurrentPage: page, Pages: pages, Total: total,
		URL: u.Entries(), StoreURL: u.Entries(), CloseDueURL: u.CloseDue(),
		Today: string(m.today()),
	})
}

// costs answers the declared costs.
func (m *Module) costs(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	views, err := m.svc.Costs(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	return ctx.JSON(stdhttp.StatusOK, costsResource(views))
}

// myInvoices answers the invoices of the investor who is asking.
func (m *Module) myInvoices(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	me, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	list, err := m.svc.InvoiceViews(ctx.Ctx(), who, me.ID)
	if err != nil {
		return err
	}
	return ctx.JSON(stdhttp.StatusOK, invoiceViewsResource(list))
}

// withdrawInvoice asks for one invoice to be paid out, whole, to the account
// given.
func (m *Module) withdrawInvoice(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	view, err := m.svc.WithdrawInvoice(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("account_id"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Me())
	}
	return ctx.JSON(stdhttp.StatusCreated, invoiceViewsResource([]InvoiceView{*view}))
}

// invoices answers one investor's invoices, for the admin.
func (m *Module) invoices(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	list, err := m.svc.InvoiceViews(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(stdhttp.StatusOK, invoiceViewsResource(list))
}

// spreadsheet streams an .xlsx the report writes.
func spreadsheet(ctx *fhttp.Context, name string, write func(io.Writer) error) error {
	var body bytes.Buffer
	if err := write(&body); err != nil {
		return err
	}
	header := ctx.Response.Header()
	header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	header.Set("Cache-Control", "private, no-store")
	ctx.Response.WriteHeader(stdhttp.StatusOK)
	_, err := ctx.Response.Write(body.Bytes())
	return err
}

func yearMonth(ctx *fhttp.Context) (int, int) {
	year, _ := strconv.Atoi(ctx.Param("year"))
	month, _ := strconv.Atoi(ctx.Param("month"))
	return year, month
}

// myMonthlyReport streams the asking investor's report of a month.
func (m *Module) myMonthlyReport(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	me, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	year, month := yearMonth(ctx)
	return spreadsheet(ctx, fmt.Sprintf("relatorio-%04d-%02d.xlsx", year, month), func(w io.Writer) error {
		return m.svc.MonthlyReport(ctx.Ctx(), who, me.ID, year, month, w)
	})
}

// myAnnualReport streams the asking investor's income report of a year.
func (m *Module) myAnnualReport(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	me, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	year, _ := yearMonth(ctx)
	return spreadsheet(ctx, fmt.Sprintf("informe-de-rendimentos-%04d.xlsx", year), func(w io.Writer) error {
		return m.svc.AnnualReport(ctx.Ctx(), who, me.ID, year, w)
	})
}

// monthlyReport streams one investor's report of a month, for the admin.
func (m *Module) monthlyReport(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	year, month := yearMonth(ctx)
	return spreadsheet(ctx, fmt.Sprintf("relatorio-%04d-%02d.xlsx", year, month), func(w io.Writer) error {
		return m.svc.MonthlyReport(ctx.Ctx(), who, ctx.Param("id"), year, month, w)
	})
}

// annualReport streams one investor's income report of a year, for the admin.
func (m *Module) annualReport(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	year, _ := yearMonth(ctx)
	return spreadsheet(ctx, fmt.Sprintf("informe-de-rendimentos-%04d.xlsx", year), func(w io.Writer) error {
		return m.svc.AnnualReport(ctx.Ctx(), who, ctx.Param("id"), year, w)
	})
}

// centavos reads an amount in centavos from a field, or in reais from the
// field of the same name ending in _brl, which is what a form types.
func centavos(ctx *fhttp.Context, field string) int64 {
	if raw := ctx.Input(field + "_brl"); raw != "" {
		v, _ := parseReais(raw)
		return v
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(ctx.Input(field)), 10, 64)
	return v
}

// storeEntry records an entry typed by the admin.
func (m *Module) storeEntry(ctx *fhttp.Context) error {
	at, err := m.occurredAt(ctx)
	if err != nil {
		return err
	}
	who, _ := ctx.User()
	e, err := m.svc.RecordManualEntry(ctx.Ctx(), who, EntryRequest{
		ExternalRef: ctx.Input("external_ref"), Kind: EntryKind(ctx.Input("kind")),
		ProductID: ctx.Input("product_id"), OccurredAt: at,
		Gross: centavos(ctx, "gross"), Fee: centavos(ctx, "fee"),
		FeeDetail: ctx.Input("fee_detail"), Description: ctx.Input("description"),
	})
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Entries())
	}
	return ctx.JSON(stdhttp.StatusCreated, entryResource{*e})
}

// occurredAt is when a typed entry happened: the instant the API sends, or
// noon of the day a form sends, in the application's timezone.
func (m *Module) occurredAt(ctx *fhttp.Context) (time.Time, error) {
	if raw := ctx.Input("occurred_at"); raw != "" {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, validation.Errors{"occurred_at": {"must be a date and time in RFC 3339"}}
		}
		return at, nil
	}
	day, err := ParseDay(ctx.Input("occurred_on"))
	if err != nil || day.IsZero() {
		return time.Time{}, validation.Errors{"occurred_on": {"must be a day in the form " + DayLayout}}
	}
	t := day.Time()
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, m.cfg.Location), nil
}

// reverseEntry undoes an entry.
func (m *Module) reverseEntry(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	e, err := m.svc.ReverseEntry(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Entries())
	}
	return ctx.JSON(stdhttp.StatusCreated, entryResource{*e})
}

// storeCost declares a cost.
func (m *Module) storeCost(ctx *fhttp.Context) error {
	if err := ctx.Request.ParseForm(); err != nil {
		return validation.Errors{"form": {"could not be read"}}
	}
	products := splitProducts(ctx.Request.PostForm["products"])
	who, _ := ctx.User()
	c, err := m.svc.CreateCost(ctx.Ctx(), who, CostRequest{
		Name: ctx.Input("name"), Category: ctx.Input("category"), Amount: centavos(ctx, "amount"),
		Recurrence: Recurrence(ctx.Input("recurrence")), StartsOn: ctx.Input("starts_on"),
		EndsOn: ctx.Input("ends_on"), AllProducts: len(products) == 0 && (ctx.Input("all_products") == "1" || ctx.Input("all_products") == "true"),
		Products: products,
	})
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().AdminCosts())
	}
	return ctx.JSON(stdhttp.StatusCreated, costsResource([]CostView{{Item: *c}}))
}

// endCost ends a cost on the day given.
func (m *Module) endCost(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	c, err := m.svc.EndCost(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("ends_on"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().AdminCosts())
	}
	return ctx.JSON(stdhttp.StatusOK, costsResource([]CostView{{Item: *c}}))
}

// closeContract closes one contract's period up to ?as_of (today when absent).
func (m *Module) closeContract(ctx *fhttp.Context) error {
	asOf, err := m.asOf(ctx)
	if err != nil {
		return err
	}
	who, _ := ctx.User()
	inv, err := m.svc.Close(ctx.Ctx(), who, ctx.Param("id"), asOf)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Investor(inv.InvestorID))
	}
	return ctx.JSON(stdhttp.StatusCreated, invoicesResource([]*Invoice{inv}))
}

// closeDue closes every contract due on the day.
func (m *Module) closeDue(ctx *fhttp.Context) error {
	asOf, err := m.asOf(ctx)
	if err != nil {
		return err
	}
	who, _ := ctx.User()
	closed, failed, err := m.svc.CloseDue(ctx.Ctx(), who, asOf)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Entries())
	}
	return ctx.JSON(stdhttp.StatusOK, closingsResource{closed: closed, failed: failed})
}

// storePayoutAccount declares an account the asking investor is paid into: a
// PIX key, or a bank account when the form says kind=bank. A form without a
// kind is a PIX key, which is what earlier releases took.
func (m *Module) storePayoutAccount(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	var (
		account *wallet.PayoutAccount
		err     error
	)
	if ctx.Input("kind") == PayoutBank {
		account, err = m.svc.AddBankAccount(ctx.Ctx(), who, BankAccountRequest{
			Bank: ctx.Input("bank"), Branch: ctx.Input("branch"),
			Number: ctx.Input("account_number"), Type: ctx.Input("account_type"),
		})
	} else {
		account, err = m.svc.AddPixAccount(ctx.Ctx(), who, ctx.Input("pix_key_type"), ctx.Input("pix_key"))
	}
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Me())
	}
	return ctx.JSON(stdhttp.StatusCreated, fieldsResource{"id": account.ID, "kind": account.Kind, "label": account.Label})
}

// invoiceReceipt streams the receipt of an invoice's paid withdrawal.
func (m *Module) invoiceReceipt(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, file, err := m.svc.InvoiceReceipt(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	defer file.Body.Close()
	header := ctx.Response.Header()
	header.Set("Content-Type", req.ReceiptType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": req.ReceiptName}))
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	ctx.Response.WriteHeader(stdhttp.StatusOK)
	_, err = io.Copy(ctx.Response, file.Body)
	return err
}

// withdrawals is the operator's queue of investors' withdrawals, filtered by
// ?status=.
func (m *Module) withdrawals(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	status := wallet.WithdrawalStatus(ctx.Query("status"))
	list, err := m.svc.Withdrawals(ctx.Ctx(), who, status)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		items := make([]map[string]any, 0, len(list))
		for _, w := range list {
			items = append(items, map[string]any{
				"id": w.Request.ID, "invoice_id": w.Invoice.ID, "investor_id": w.Invoice.InvestorID,
				"amount": int64(w.Request.Amount), "status": string(w.Request.Status),
				"requested_at": w.Request.RequestedAt.UTC().Format(time.RFC3339),
			})
		}
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"items": items})
	}
	sc, err := m.screen(ctx, who, "screen.admin_withdrawals_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewAdminWithdrawal, AdminWithdrawalsPageData{
		Screen: sc, Rows: withdrawalRows(m.URLs(), sc.Labels, list, m.cfg.Location),
		Status: string(status), URL: m.URLs().Withdrawals(),
	})
}

// startWithdrawal takes an investor's withdrawal to be paid.
func (m *Module) startWithdrawal(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, err := m.svc.StartPaying(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Withdrawals())
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// payWithdrawal records that an investor's withdrawal was paid, with the
// end-to-end identifier of the PIX and the receipt as the part named
// "receipt" of a multipart form.
func (m *Module) payWithdrawal(ctx *fhttp.Context) error {
	ctx.Request.Body = stdhttp.MaxBytesReader(ctx.Response, ctx.Request.Body, MaxContractBytes+(1<<20))
	if err := ctx.Request.ParseMultipartForm(MaxContractBytes); err != nil {
		return validation.Errors{"receipt": {"could not be read; send the form as multipart/form-data, under the size limit"}}
	}
	in := wallet.PaymentInput{RequestID: ctx.Param("id"), EndToEndID: strings.TrimSpace(ctx.Request.FormValue("end_to_end_id"))}
	file, header, err := ctx.Request.FormFile("receipt")
	if err != nil {
		return validation.Errors{"receipt": {"is required"}}
	}
	defer file.Close()
	in.ReceiptName = header.Filename
	in.ReceiptType = header.Header.Get("Content-Type")
	who, _ := ctx.User()
	req, err := m.svc.PayInvestorWithdrawal(ctx.Ctx(), who, in, file)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Withdrawals())
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// rejectWithdrawal refuses an investor's withdrawal with a reason.
func (m *Module) rejectWithdrawal(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, err := m.svc.RejectInvestorWithdrawal(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.URLs().Withdrawals())
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// asOf is the day a closing runs as of: the input's, or today in the
// application's timezone.
func (m *Module) asOf(ctx *fhttp.Context) (Day, error) {
	if raw := ctx.Input("as_of"); raw != "" {
		d, err := ParseDay(raw)
		if err != nil {
			return "", validation.Errors{"as_of": {"must be a day in the form " + DayLayout}}
		}
		return d, nil
	}
	return DayOf(time.Now(), m.cfg.Location), nil
}

// Migrations declares the schema this module owns.
//
// They are returned in the order their names sort in, which is the order they
// apply in: the name carries the order, and nothing else decides it.
func (m *Module) Migrations() []foundation.Migration {
	return []foundation.Migration{
		createInvestors{}, createContracts{}, createContractProducts{},
		createEntries{}, createCostItems{}, createCostItemProducts{},
		createInvoices{}, createInvoiceEntries{},
	}
}

// The migration is reversible, and the assertion is here rather than discovered
// at rollback: the migrator tests for Down with a type assertion, so a Down
// with the wrong signature would leave a rollback that silently does nothing.
var _ migrations.ReversibleMigration = createInvestors{}

// createInvestors is the table this module owns, and the index its listing
// reads by.
type createInvestors struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order. It is fixed
// once the package is published: changing what an applied name means leaves the
// change missing everywhere it already ran, and nothing says so.
func (createInvestors) GetName() string { return "20261012_0001_create_investors" }

// Up creates the table and the index the keyset pagination scans.
//
// The Blueprint spells each column for the engine the migration is running on,
// which is what lets one application develop on a file and deploy on Postgres
// without a second schema. That used to be written out as SQL here, with a
// comment explaining that an identifier column is VARCHAR rather than TEXT
// because it takes part in a key and MySQL refuses TEXT in one without a prefix
// length -- which is the grammar's job, done by hand in every migration that
// remembered to.
//
// The timestamp has no database default: the value comes from Go.
func (createInvestors) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, investorsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("user_id", maxIdentifierLen)
		table.String("person_kind", maxShortLen)
		table.String("document", maxDocumentLen)
		table.String("name", maxNameLen)
		table.String("legal_name", maxNameLen).Default("")
		table.String("legal_representative", maxNameLen).Default("")
		table.String("email", maxEmailLen)
		table.String("status", maxShortLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// One investor per document and one per login user, per tenant. Unique
		// indexes and not checks in Go, because a check in Go is a check two
		// concurrent requests both pass.
		table.Unique([]string{"tenant_id", "document"}, "investors_tenant_document_uq")
		table.Unique([]string{"tenant_id", "user_id"}, "investors_tenant_user_uq")

		// The index matches the ORDER BY of the listing, tenant first. Without
		// it every page is a scan of every customer's rows.
		table.Index([]string{"tenant_id", "created_at", "id"}, "investors_tenant_created_idx")
	})
}

// Down drops the table, which takes its indexes with it.
func (createInvestors) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, investorsTable)
}

var (
	_ migrations.ReversibleMigration = createContracts{}
	_ migrations.ReversibleMigration = createContractProducts{}
)

// createContracts is the table of contract versions.
type createContracts struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createContracts) GetName() string { return "20261012_0002_create_investor_contracts" }

// Up creates the table. A day is text in YYYY-MM-DD, so no engine moves it to
// another day by reading it in its own timezone.
func (createContracts) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, contractsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("investor_id", maxIdentifierLen)
		table.String("number", maxShortLen)
		table.UnsignedSmallInteger("version").Default(1)
		table.String("root_id", maxIdentifierLen)
		table.String("signed_on", len(DayLayout))
		table.String("starts_on", len(DayLayout))
		table.String("ends_on", len(DayLayout)).Default("")
		table.BigInteger("share_bps")
		table.UnsignedSmallInteger("billing_day")
		table.UnsignedSmallInteger("all_products").Default(0)
		table.String("status", maxShortLen)
		table.String("file_key", maxTextLen)
		table.String("file_name", maxNameLen)
		table.String("file_type", maxShortLen)
		table.BigInteger("file_size")
		table.String("file_sha256", maxIdentifierLen)
		table.String("created_by", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// One row per version of a contract. Two concurrent amendments of the
		// same version both read version N and both write N+1; the index is what
		// refuses the second.
		table.Unique([]string{"tenant_id", "root_id", "version"}, "investor_contracts_version_uq")
		table.Index([]string{"tenant_id", "investor_id", "created_at"}, "investor_contracts_investor_idx")
		table.Index([]string{"tenant_id", "status"}, "investor_contracts_status_idx")
	})
}

// Down drops the table.
func (createContracts) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, contractsTable)
}

// createContractProducts is the table of products a contract counts revenue of.
type createContractProducts struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createContractProducts) GetName() string {
	return "20261012_0003_create_investor_contract_products"
}

// Up creates the table, with a product named once per contract version.
func (createContractProducts) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, contractProductsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("contract_id", maxIdentifierLen)
		table.String("product_id", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		table.Unique([]string{"tenant_id", "contract_id", "product_id"}, "investor_contract_products_uq")
	})
}

// Down drops the table.
func (createContractProducts) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, contractProductsTable)
}

var (
	_ migrations.ReversibleMigration = createEntries{}
	_ migrations.ReversibleMigration = createCostItems{}
	_ migrations.ReversibleMigration = createCostItemProducts{}
	_ migrations.ReversibleMigration = createInvoices{}
	_ migrations.ReversibleMigration = createInvoiceEntries{}
)

// createEntries is the table of the application's money movements.
type createEntries struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createEntries) GetName() string { return "20261012_0004_create_investor_entries" }

// Up creates the table. Money is BIGINT centavos, never a decimal or a float.
func (createEntries) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, entriesTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("product_id", maxIdentifierLen)
		table.String("kind", maxShortLen)
		table.Timestamp("occurred_at")
		table.String("occurred_on", len(DayLayout))
		table.BigInteger("gross")
		table.BigInteger("fee")
		table.BigInteger("net")
		table.String("fee_detail", maxShortLen).Default("")
		table.String("description", maxTextLen).Default("")
		table.String("source", maxShortLen)
		table.String("external_ref", maxNameLen)
		table.String("reverses_id", maxIdentifierLen).Default("")
		table.String("created_by", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// The idempotency of RecordEntry: one entry per event per tenant.
		table.Unique([]string{"tenant_id", "external_ref"}, "investor_entries_ref_uq")
		table.Index([]string{"tenant_id", "occurred_on", "product_id"}, "investor_entries_day_idx")
	})
}

// Down drops the table.
func (createEntries) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, entriesTable)
}

// createCostItems is the table of declared costs.
type createCostItems struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createCostItems) GetName() string { return "20261012_0005_create_investor_cost_items" }

// Up creates the table.
func (createCostItems) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, costItemsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("name", maxNameLen)
		table.String("category", maxShortLen)
		table.BigInteger("amount")
		table.String("recurrence", maxShortLen)
		table.String("starts_on", len(DayLayout))
		table.String("ends_on", len(DayLayout)).Default("")
		table.UnsignedSmallInteger("all_products").Default(0)
		table.String("created_by", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		table.Index([]string{"tenant_id", "starts_on"}, "investor_cost_items_start_idx")
	})
}

// Down drops the table.
func (createCostItems) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, costItemsTable)
}

// createCostItemProducts is the table of products a scoped cost is borne by.
type createCostItemProducts struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createCostItemProducts) GetName() string {
	return "20261012_0006_create_investor_cost_item_products"
}

// Up creates the table.
func (createCostItemProducts) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, costItemProductsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("cost_item_id", maxIdentifierLen)
		table.String("product_id", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		table.Unique([]string{"tenant_id", "cost_item_id", "product_id"}, "investor_cost_item_products_uq")
	})
}

// Down drops the table.
func (createCostItemProducts) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, costItemProductsTable)
}

// createInvoices is the table of closings.
type createInvoices struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createInvoices) GetName() string { return "20261012_0007_create_investor_invoices" }

// Up creates the table.
func (createInvoices) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, invoicesTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("investor_id", maxIdentifierLen)
		table.String("contract_id", maxIdentifierLen)
		table.String("root_id", maxIdentifierLen)
		table.String("period_start", len(DayLayout))
		table.String("period_end", len(DayLayout))
		table.BigInteger("gross")
		table.BigInteger("fees")
		table.BigInteger("costs")
		table.BigInteger("net")
		table.BigInteger("cost_ratio_ppm")
		table.BigInteger("share_bps")
		table.BigInteger("amount")
		table.String("status", maxShortLen)
		table.String("wallet_operation_id", maxIdentifierLen).Default("")
		table.String("withdrawal_request_id", maxIdentifierLen).Default("")
		table.UnsignedSmallInteger("withdrawal_attempts").Default(0)
		table.String("closed_by", maxIdentifierLen)
		table.Timestamp("closed_at")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// One invoice per contract and period: closing twice, or two admins
		// closing at once, makes one.
		table.Unique([]string{"tenant_id", "root_id", "period_end"}, "investor_invoices_period_uq")
		table.Index([]string{"tenant_id", "investor_id", "period_end"}, "investor_invoices_investor_idx")
	})
}

// Down drops the table.
func (createInvoices) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, invoicesTable)
}

// createInvoiceEntries is the table of which entries each invoice counted.
type createInvoiceEntries struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createInvoiceEntries) GetName() string { return "20261012_0008_create_investor_invoice_entries" }

// Up creates the table. An entry is counted once per contract, ever: the
// unique index is what keeps a late entry from being counted twice.
func (createInvoiceEntries) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, invoiceEntriesTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("invoice_id", maxIdentifierLen)
		table.String("root_id", maxIdentifierLen)
		table.String("entry_id", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		table.Unique([]string{"tenant_id", "root_id", "entry_id"}, "investor_invoice_entries_uq")
		table.Index([]string{"tenant_id", "invoice_id"}, "investor_invoice_entries_invoice_idx")
	})
}

// Down drops the table.
func (createInvoiceEntries) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, invoiceEntriesTable)
}
