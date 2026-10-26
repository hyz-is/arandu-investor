package investor

import (
	"embed"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/arandu-io/framework/foundation"
	fview "github.com/arandu-io/framework/view"
	hview "github.com/arandu-io/hesape/view"
)

// The view sources this package hands to the project that installs it.
//
// They are embedded rather than read off disk because what a module publishes
// is a tree it carries, not a directory it points at: the program that writes
// the files is the application's own binary, running somewhere this repository
// does not exist as files. Whatever is handed over has to already be inside the
// program doing the handing.
//
// The paths inside the archive are the paths the files take in the project, not
// paths of this repository that something has to translate on the way. A view
// sits here at the address it will sit at there, so the publication names
// neither a source directory nor a destination one, and there is no second
// spelling of a destination for the first one to disagree with.
//
//go:embed resources/views
var viewSources embed.FS

// Where a view is written and what it is called.
//
// viewRoot is the directory every path in the archive starts with, and it is
// what is cut off to get the name a view is registered under. viewSuffix is the
// extension: it ends in .go so the build tag on the first line keeps the
// compiler out of a file that is markup below the package clause.
const (
	viewRoot   = "resources/views"
	viewSuffix = ".kyse.go"
)

// compiledRoot is where the view compiler writes the Go it produces, mirroring
// the tree of the sources. It is build output: gitignored, rebuilt on demand,
// and never edited.
const compiledRoot = "storage/framework/views"

// moduleDir is the directory an application keeps installed packages' views in.
//
// It is part of the path in the archive and not something the publication adds,
// which is what makes the archive a literal picture of what lands in the
// project. Two packages with a view called index are two files under two names
// below it, and neither shadows the other or the application's own.
//
// It is not called vendor, and that is the one thing about this name worth
// writing down. The go command reserves that word twice over, and a tree of
// published views hits both rules:
//
//   - a file under a directory named vendor is left out of the module zip at
//     any depth, so it stays in this repository and is missing for everyone who
//     downloads the module. The embed above then matches nothing, and what the
//     person building the project reads is "pattern resources/views: no
//     matching files found" -- an error about this package, raised in theirs.
//   - a package whose import path carries the element cannot be imported at
//     all: "use of vendored package not allowed". A published view is compiled
//     into a Go package the application has to import for its init() to
//     register anything, so the second rule refuses exactly the import Boot
//     asks for.
//
// Both were measured, not reasoned about, and the second is why the name could
// not be fixed on this side alone: the destination is the address the
// application looks a package's views up at, and it is the destination that
// carried the word.
const moduleDir = "modules"

// Publishes declares the files this package offers, each at the path it takes
// relative to the root of the project.
//
// One tree, under one tag. A publication may carry a page, a component, a
// configuration file, a migration, a catalogue of sentences or an asset, and
// this package offers the first of those and nothing else. Each absence is a
// decision rather than an omission:
//
//   - configuration is the Config struct New is handed, checked by the compiler
//     and validated before the module exists. A file copied into the project
//     beside it would be a second place to say the same thing, and only one of
//     the two could be the one the code reads.
//   - a stylesheet, a script or any other asset is registered with the view
//     layer and served from an address derived from its own bytes. Copying one
//     into the project would put a second copy of those bytes under a second
//     address, and a page can only reference one of them.
//   - translations are overridden by writing the lines the application wants
//     into its own override tree, which the catalogue loader already reads. A
//     copy of every line this package ships is a copy that goes stale, and it
//     goes stale without saying so.
//   - migrations are declared and collected, never copied. A copy in the
//     project's own migration directory is found by the runner as well, so one
//     schema change applies twice under two names.
//
// What is left is the markup, and it is here for the reason the others are not:
// it is the one thing the project is expected to edit. A package cannot know
// what a screen should say in a product it has never seen.
func (m *Module) Publishes() []foundation.Publication {
	return []foundation.Publication{{Tag: foundation.PublishView, Files: viewSources}}
}

// PublishedPaths are the files in the archive, each relative to the root of the
// application, sorted.
func PublishedPaths() []string { return append([]string(nil), publishedPaths...) }

// ViewNames are the names the published views are rendered by, sorted.
//
// The name is the path under the view root with its separators turned into
// dots, which is what the view compiler writes into the registration call. It
// is derived from the same archive the publication carries, so a view that was
// renamed cannot keep an old name here.
func ViewNames() []string { return append([]string(nil), viewNames...) }

// ViewPackages are the directories the compiled views land in, each relative to
// the root of the application, sorted and without repeats.
//
// Importing one is what puts its views in the binary: a compiled view calls the
// registry from init(), and a package nothing imports is not linked at all. The
// import is named rather than written into bootstrap/app.go, because one line
// somebody reads beats a file that changed while they were not looking.
func ViewPackages() []string {
	var out []string
	seen := make(map[string]bool)
	for _, path := range publishedPaths {
		dir := compiledRoot + strings.TrimPrefix(path[:strings.LastIndexByte(path, '/')], viewRoot)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// The archive, read once at load.
//
// A failure here is a broken binary rather than a condition to recover from:
// the files are compiled in, so either they are all there or the build that
// produced this program was wrong.
var publishedPaths, viewNames = readArchive()

func readArchive() (paths, names []string) {
	err := fs.WalkDir(viewSources, viewRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, viewSuffix) {
			return nil
		}
		paths = append(paths, path)
		names = append(names, viewName(path))
		return nil
	})
	if err != nil {
		panic("investor: reading the embedded views: " + err.Error())
	}
	if len(paths) == 0 {
		panic("investor: the embedded view directory holds no view, so every name this package renders would be missing and nothing would say so")
	}
	return paths, names
}

// viewName turns an archive path into the name the view is registered under.
//
//	resources/views/modules/investor/index.kyse.go -> modules.investor.index
func viewName(path string) string {
	name := strings.TrimPrefix(strings.TrimPrefix(path, viewRoot), "/")
	name = strings.TrimSuffix(name, viewSuffix)
	return strings.ReplaceAll(name, "/", ".")
}

// The screens this package renders, by the name their path gives them.
const (
	ViewDashboard       = "modules.investor.dashboard"
	ViewStatement       = "modules.investor.statement"
	ViewProjection      = "modules.investor.projection"
	ViewAdminInvestors  = "modules.investor.admin.investors"
	ViewAdminInvestor   = "modules.investor.admin.investor"
	ViewAdminCosts      = "modules.investor.admin.costs"
	ViewAdminEntries    = "modules.investor.admin.entries"
	ViewAdminWithdrawal = "modules.investor.admin.withdrawals"
)

// FormState is what a form field reads from the page it is drawn on: what was
// typed before a refusal, and the message for each field.
type FormState struct{ hview.Page }

// FieldError is the first message for one field, empty when it was accepted.
func (f FormState) FieldError(name string) string { return f.Page.FieldError(name) }

// Screen is what every page of this package carries: the page hesape's
// view.New builds, the application's chrome, the sentences, and where the
// package is mounted.
//
// It embeds hesape's view.Page, so every page of this package is a
// hesape/view.Layout -- what the layouts of an application on Arandu
// framework v0.62 and newer assert -- and it answers HasErrors and
// ErrorSummary as well, so it is also the framework/view.Layout an older
// layout asks for.
type Screen struct {
	hview.Page
	Labels Labels
	// Prefix is where the investor's routes are mounted. URLs has every
	// address; Prefix is kept for a published view that still reads it.
	Prefix string
	// URLs are the addresses of the package's routes.
	URLs URLs

	chrome any
}

// Compile-time proof that a page of this package fits both layout contracts.
var (
	_ hview.Layout   = Screen{}
	_ fview.Layout   = Screen{}
	_ slog.LogValuer = Screen{}
)

// Chrome is what Config.Chrome answered for this screen: the application's
// sidebar, navigation and account menu, in whatever type its layout draws
// from. A layout asserts that type on it; nil when Config.Chrome is nil.
func (s Screen) Chrome() any { return s.chrome }

// HasErrors says whether the attempt that landed here was rejected:
// framework/view.Layout's name for hesape's Any.
func (s Screen) HasErrors() bool { return s.Page.Any() }

// All is one line per rejected message, for the banner above a form, in the
// screen's language: the field is named by its label in the catalogue
// ("Chave PIX: Preencha este campo."), where hesape's page would name it by
// its input ("Pix key").
func (s Screen) All() []string { return s.Labels.Summary(s.Page.Errors) }

// ErrorSummary is one line per rejected field: framework/view.Layout's name
// for All.
func (s Screen) ErrorSummary() []string { return s.All() }

// FieldErrors is every message for one field.
func (s Screen) FieldErrors(name string) []string { return s.Page.Get(name) }

// MarshalJSON writes the page with the CSRF token redacted, as
// framework/view.Page does: a page dumped for debugging never publishes the
// token. It answers for every page data that embeds Screen.
func (s Screen) MarshalJSON() ([]byte, error) { return framePage(s.Page).MarshalJSON() }

// LogValue is the page as a log line carries it, the token redacted.
func (s Screen) LogValue() slog.Value { return framePage(s.Page).LogValue() }

// Form is the form state of the page.
func (s Screen) Form() FormState { return FormState{Page: s.Page} }

// HasErrors says whether the attempt that landed here was rejected.
func (f FormState) HasErrors() bool { return f.Page.Any() }

// ErrorSummary is one line per rejected field.
func (f FormState) ErrorSummary() []string { return f.Page.All() }

// FieldErrors is every message for one field.
func (f FormState) FieldErrors(name string) []string { return f.Page.Get(name) }

// MarshalJSON writes the form state with the CSRF token redacted.
func (f FormState) MarshalJSON() ([]byte, error) { return framePage(f.Page).MarshalJSON() }

// LogValue is the form state as a log line carries it, the token redacted.
func (f FormState) LogValue() slog.Value { return framePage(f.Page).LogValue() }

// framePage copies a hesape page into framework/view.Page, by name, for the
// two methods that redact the token there.
func framePage(p hview.Page) fview.Page {
	return fview.Page{
		Title: p.Title, Description: p.Description, Canonical: p.Canonical, AppName: p.AppName, Token: p.Token,
		Authenticated: p.Authenticated, UserName: p.UserName,
		HomeURL: p.HomeURL, LoginURL: p.LoginURL, LogoutURL: p.LogoutURL, RegisterURL: p.RegisterURL,
		PanelURL: p.PanelURL, AdminURL: p.AdminURL,
		Impersonating: p.Impersonating, ImpersonationActs: p.ImpersonationActs, Impersonator: p.Impersonator,
		LeaveImpersonationURL: p.LeaveImpersonationURL,
		Path:                  p.Path, Errors: p.Errors, Old: p.Old, Messages: p.Messages,
	}
}

// ContractRow is one contract as a screen shows it.
type ContractRow struct {
	// Ends is the last day, or "em aberto" for a contract with none; Period
	// is the two together, as a column draws them.
	ID, Number, Share, Products, Status, Starts, Ends, Period string
	Version, BillingDay                                       int
	Active                                                    bool
	StatementURL, ProjectionURL, FileURL                      string
	CloseURL, AmendURL                                        string
}

// InvoiceRow is one invoice as a screen shows it.
type InvoiceRow struct {
	ID, Period, Gross, Fees, Costs, Net, Share, Amount string
	Status, StatusKey, Withdrawal, WithdrawalKey       string
	Withdrawable                                       bool
	WithdrawURL, ReceiptURL, RejectedReason            string
}

// AccountOption is one payout account a withdrawal can be paid into: its
// label, and its kind, PayoutPix or PayoutBank.
type AccountOption struct{ ID, Label, Kind string }

// DashboardPageData is the investor's own panel.
type DashboardPageData struct {
	Screen
	Investor                                      string
	Document                                      string
	Available, Held, ReceivedYear, WithdrawnTotal string
	Contracts                                     []ContractRow
	Invoices                                      []InvoiceRow
	Accounts                                      []AccountOption
	Year, Month                                   int
	// ReportsURL is the base of the reports; PixURL, the address a payout
	// account is declared at, is the same as PayoutAccountsURL and kept for a
	// published view that still reads it.
	ReportsURL, PixURL, PayoutAccountsURL string
	// ReportMonths and ReportYears are the reports offered: none before the
	// investor's first contract started.
	ReportMonths, ReportYears []ReportLink
}

// StatementRow is one line of a statement.
type StatementRow struct {
	ID, Day, Product, Kind, Description                 string
	Gross, Fee, FeeDetail, Cost, Net                    string
	Estimate                                            bool
	GrossSort, FeeSort, CostSort, NetSort, DaySortValue string
}

// StatementPageData is one contract's statement.
type StatementPageData struct {
	Screen
	Contract                ContractRow
	Rows                    []StatementRow
	Gross, Fees, Costs, Net string
	From, To, Kind, Product string
	URL                     string
	HasEstimates            bool
}

// ProjectedCostRow is one declared cost in the open period.
type ProjectedCostRow struct{ Name, Category, Recurrence, Scope, Amount string }

// ProjectionPageData is one contract's open period.
type ProjectionPageData struct {
	Screen
	Contract                           ContractRow
	PeriodStart, PeriodEnd             string
	Costs                              []ProjectedCostRow
	Gross, Fees, ProjectedCosts, Ratio string
	EstimatedNet, EstimatedAmount      string
}

// InvestorRow is one investor in the admin's registry.
type InvestorRow struct {
	ID, Name, Document, Kind, Email, Status, User, URL string
	Active                                             bool
}

// AdminInvestorsPageData is the admin's registry.
type AdminInvestorsPageData struct {
	Screen
	Rows          []InvestorRow
	StoreURL      string
	Next, NextURL string
}

// AdminInvestorPageData is one investor, for the admin.
type AdminInvestorPageData struct {
	Screen
	Investor                       InvestorRow
	LegalName, LegalRepresentative string
	Contracts                      []ContractRow
	Invoices                       []InvoiceRow
	ContractStoreURL               string
	UpdateURL, SuspensionURL       string
	ReportsURL                     string
	Year, Month                    int
	Today                          string
}

// CostRow is one declared cost.
type CostRow struct {
	ID, Name, Category, Amount, Recurrence, Starts, Ends, Scope, EndURL string
	Ended                                                               bool
}

// AdminCostsPageData is the declared costs, for the admin.
type AdminCostsPageData struct {
	Screen
	Rows            []CostRow
	StoreURL, Today string
}

// EntryRow is one entry in the admin's books.
type EntryRow struct {
	ID, Day, Product, Kind, Description, Gross, Fee, Net, Source, Ref, ReverseURL string
	Reversible                                                                    bool
	GrossSort, DaySort                                                            string
}

// AdminEntriesPageData is the admin's books, a page at a time.
type AdminEntriesPageData struct {
	Screen
	Rows                              []EntryRow
	Query, Kind, Sort, Dir            string
	CurrentPage, Pages, Total         int
	URL, StoreURL, CloseDueURL, Today string
	// PageSize is how many rows one page holds, the size the server reads
	// the books with; the table's footer and its pager count by it.
	PageSize int
	// Hide are the keys of the columns the reader switched off, as the
	// address carried them (hide=source&hide=ref), limited to the columns
	// that can be hidden. The table draws its state from them.
	Hide []string
}

// WithdrawalRow is one investor's withdrawal request in the operator's queue.
type WithdrawalRow struct {
	ID, Investor, Amount, Account, Status, StatusKey, RequestedAt, Reason string
	Open, Processing, Paid                                                bool
	ProcessURL, PayURL, RejectURL, ReceiptURL                             string
}

// AdminWithdrawalsPageData is the queue of investors' withdrawals.
type AdminWithdrawalsPageData struct {
	Screen
	Rows   []WithdrawalRow
	Status string
	URL    string
}
