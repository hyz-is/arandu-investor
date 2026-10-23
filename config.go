package investor

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/translation"
	hview "github.com/arandu-io/hesape/view"
	activitylog "github.com/hyz-is/arandu-activitylog"
	wallet "github.com/hyz-is/arandu-wallet"
)

// The defaults for the optional settings. They are constants rather than
// literals inside Config.withDefaults, so the value a reader finds here is the
// value the package uses.
const (
	// DefaultPrefix is where the routes are mounted when Config leaves Prefix
	// empty.
	DefaultPrefix = "/investor"
	// DefaultPageSize is how many records one page answers with when Config
	// leaves PageSize at zero.
	DefaultPageSize = 25
	// MaxPageSize is the ceiling PageSize is refused above. A page nobody
	// bounded is a page that reads the whole table on the day the table is
	// large.
	MaxPageSize = 200
)

// Config is what the application passes when it wires this package.
//
// A typed struct rather than a map: a misspelled key in a map is a setting that
// silently keeps its default, and the failure shows up as behaviour nobody
// asked for rather than as an error. Here a field that does not exist does not
// compile.
//
// It names no tenant, and that is deliberate. Every route sits behind
// RequireAuth, so the tenant of every statement is the one the Grant carries,
// from the session of whoever is asking; a tenant written here would be a
// second place for one to come from. The zero value is a valid configuration.
type Config struct {
	// Prefix is the path the routes are mounted under. Empty means
	// DefaultPrefix.
	Prefix string

	// PageSize is how many records one page answers with. Zero means
	// DefaultPageSize, and anything above MaxPageSize is refused rather than
	// clamped: a number somebody wrote and did not get is worse than a number
	// somebody wrote and was told about.
	PageSize int

	// Files is the Disk the signed contracts are kept on: the application's,
	// local or S3, reached with the Grant so every key carries the tenant's
	// prefix. Required: a contract without its signed copy is the one thing
	// this package exists to keep.
	Files *filesystem.Disk

	// Location is the timezone an instant becomes a calendar day in, which is
	// what a period counts by: a sale at 22:00 in Sao Paulo on the 19th is a
	// sale of the 19th, though it is already the 20th in UTC. Nil means
	// DefaultLocation.
	Location *time.Location

	// Wallet is arandu-wallet's service, the application's one instance
	// (walletModule.Service()). Required: an invoice is deposited in the
	// investor's wallet and withdrawn from it, and this package keeps no
	// balance of its own.
	Wallet *wallet.WalletService

	// Payer is the company that pays the investors, printed on the yearly
	// income report. Optional: left empty, the report prints a dash.
	Payer Payer

	// Translator is the application's own catalogue, asked before the one this
	// package ships, key by key: "investor.screen.dashboard_title". Optional.
	Translator *translation.Translator

	// Paths are the segments every route is built from, under Prefix and
	// AdminPrefix. A field left empty keeps its DefaultPaths value, so the zero
	// value answers at the addresses earlier releases did; PortuguesePaths is
	// the whole set in Brazilian Portuguese.
	Paths Paths

	// AdminPrefix is where the admin's routes are mounted. Empty means Prefix
	// followed by Paths.Admin, which is where earlier releases put them. An
	// application whose team works on another side of the site -- an operator
	// console at /admin -- mounts them there.
	AdminPrefix string

	// Middleware runs on every route of this package after RequireAuth, in
	// order, and AdminMiddleware after it on the admin's routes only. An
	// application whose permissions arandu-permission administers mounts its
	// Roles() in Middleware: RequireAuth puts the subject as the session
	// resolved it, without the actions arandu-permission resolves on each
	// request. A guard of the application's own side -- the operator console's
	// -- goes in AdminMiddleware. Nil runs nothing more. A guard answers "is
	// this request on the right side" and stops; the policy still decides.
	Middleware      []func(http.Handler) http.Handler
	AdminMiddleware []func(http.Handler) http.Handler

	// Chrome is what the application's layout draws around a screen of this
	// package: its sidebar, its navigation, the account menu. It is called once
	// per screen, after RequireAuth and before the page is rendered, and what it
	// answers is carried on the page as Screen.Chrome, where a layout asserts
	// the interface it draws from. An error is returned as the handler's. Nil
	// draws the screens with the page hesape's view.New builds and nothing
	// more.
	Chrome func(ctx *fhttp.Context, req ChromeRequest) (any, error)

	// Activity is arandu-activitylog's logger, the application's one instance
	// (activityModule.Logger()). Every act that changes something -- an
	// investor, a contract, an entry, a cost, a closing, a payout account, a
	// withdrawal asked for, taken, paid or rejected -- is recorded under the
	// Grant it was authorized with, in the log named ActivityLogName. Nil
	// records nothing.
	Activity *activitylog.Logger
}

// ChromeRequest is what Config.Chrome is asked about one screen.
type ChromeRequest struct {
	// Title is the screen's title, already in the request's language.
	Title string
	// Admin says the screen is one of the admin's, mounted under AdminPrefix.
	Admin bool
	// Subject is who is asking, as RequireAuth and Config.Middleware left it.
	Subject security.Subject
	// Page is the page hesape's view.New built for the request.
	Page hview.Page
}

// Paths are the segments the routes of this package are built from. Each one
// starts with "/" and holds no wildcard; Me may be "/", which mounts the
// investor's own panel at Prefix itself.
//
//	GET  Prefix + Me                                 the investor's panel
//	GET  Prefix + Me + Contracts                     their contracts
//	GET  Prefix + Me + Invoices                      their invoices
//	GET  Prefix + Me + Reports + /{year}[/{month}]   their reports
//	POST Prefix + Me + PayoutAccounts                a payout account
//	GET  Prefix + Contracts + /{id} + File|Statement|Projection
//	GET  Prefix + Costs
//	POST Prefix + Invoices + /{id} + Withdrawal      ask for an invoice
//	GET  Prefix + Invoices + /{id} + Receipt         the receipt of its payment
//
// and under AdminPrefix: Investors (with Suspension, Contracts, Invoices and
// Reports below one), Contracts + /{id} + Amendments|Closings, Closings + Due,
// Entries (with Reversal), Costs (with End), and Withdrawals (with Processing,
// Payment and Rejection).
type Paths struct {
	Me, Contracts, Invoices, Reports, PayoutAccounts string
	File, Statement, Projection, Costs               string
	Withdrawal, Receipt                              string
	Admin, Investors, Suspension, Amendments         string
	Closings, Due, Entries, Reversal, End            string
	Withdrawals, Processing, Payment, Rejection      string
}

// DefaultPaths are the English segments, the addresses of earlier releases.
var DefaultPaths = Paths{
	Me: "/me", Contracts: "/contracts", Invoices: "/invoices", Reports: "/reports", PayoutAccounts: "/payout-accounts",
	File: "/file", Statement: "/statement", Projection: "/projection", Costs: "/costs",
	Withdrawal: "/withdrawal", Receipt: "/receipt",
	Admin: "/admin", Investors: "/investors", Suspension: "/suspension", Amendments: "/amendments",
	Closings: "/closings", Due: "/due", Entries: "/entries", Reversal: "/reversal", End: "/end",
	Withdrawals: "/withdrawals", Processing: "/processing", Payment: "/payment", Rejection: "/rejection",
}

// PortuguesePaths are the segments in Brazilian Portuguese, for an
// application whose addresses are written in it: the investor's panel at
// Prefix itself, /contratos, /faturas/{id}/saque, /faturas/{id}/comprovante,
// /conta-de-recebimento, and the admin's /investidores and /saques.
var PortuguesePaths = Paths{
	Me: "/", Contracts: "/contratos", Invoices: "/faturas", Reports: "/relatorios", PayoutAccounts: "/conta-de-recebimento",
	File: "/arquivo", Statement: "/extrato", Projection: "/projecao", Costs: "/custos",
	Withdrawal: "/saque", Receipt: "/comprovante",
	Admin: "/admin", Investors: "/investidores", Suspension: "/suspensao", Amendments: "/aditivos",
	Closings: "/fechamentos", Due: "/vencidos", Entries: "/lancamentos", Reversal: "/estorno", End: "/encerramento",
	Withdrawals: "/saques", Processing: "/em-pagamento", Payment: "/pagamento", Rejection: "/recusa",
}

// fields are the segments with their names, for validating and filling them.
func (p *Paths) fields() []struct {
	name  string
	value *string
} {
	return []struct {
		name  string
		value *string
	}{
		{"Me", &p.Me}, {"Contracts", &p.Contracts}, {"Invoices", &p.Invoices}, {"Reports", &p.Reports},
		{"PayoutAccounts", &p.PayoutAccounts}, {"File", &p.File}, {"Statement", &p.Statement},
		{"Projection", &p.Projection}, {"Costs", &p.Costs}, {"Withdrawal", &p.Withdrawal}, {"Receipt", &p.Receipt},
		{"Admin", &p.Admin}, {"Investors", &p.Investors}, {"Suspension", &p.Suspension},
		{"Amendments", &p.Amendments}, {"Closings", &p.Closings}, {"Due", &p.Due}, {"Entries", &p.Entries},
		{"Reversal", &p.Reversal}, {"End", &p.End}, {"Withdrawals", &p.Withdrawals},
		{"Processing", &p.Processing}, {"Payment", &p.Payment}, {"Rejection", &p.Rejection},
	}
}

// validate reports a segment that cannot be part of a route.
func (p Paths) validate() error {
	for _, f := range p.fields() {
		v := *f.value
		if v == "" {
			continue
		}
		if v[0] != '/' || strings.ContainsAny(v, "{} \t?#") || (v == "/" && f.name != "Me") || strings.HasSuffix(v, "/") && v != "/" {
			return fmt.Errorf("investor: Config.Paths.%s is %q: a segment starts with /, ends without one and holds no wildcard (only Me may be \"/\")", f.name, v)
		}
	}
	return nil
}

// withDefaults fills every empty segment from DefaultPaths.
func (p Paths) withDefaults() Paths {
	d := DefaultPaths
	defaults := d.fields()
	for i, f := range p.fields() {
		if *f.value == "" {
			*f.value = *defaults[i].value
		}
	}
	return p
}

// DefaultLocation is the timezone days are counted in when Config leaves
// Location nil. The package is Brazilian first; an application elsewhere sets
// its own.
const DefaultLocation = "America/Sao_Paulo"

// Validate reports what the configuration cannot be used with.
//
// It is called by New, so an application with a setting that cannot work fails
// where it is wired rather than on the first request that needed it.
func (c Config) Validate() error {
	if c.Prefix != "" && c.Prefix[0] != '/' {
		return fmt.Errorf("investor: Config.Prefix is %q and has to start with /", c.Prefix)
	}
	if c.AdminPrefix != "" && (c.AdminPrefix[0] != '/' || strings.HasSuffix(c.AdminPrefix, "/")) {
		return fmt.Errorf("investor: Config.AdminPrefix is %q and has to start with / and end without one", c.AdminPrefix)
	}
	if err := c.Paths.validate(); err != nil {
		return err
	}
	for i, mw := range c.Middleware {
		if mw == nil {
			return fmt.Errorf("investor: Config.Middleware[%d] is nil", i)
		}
	}
	for i, mw := range c.AdminMiddleware {
		if mw == nil {
			return fmt.Errorf("investor: Config.AdminMiddleware[%d] is nil", i)
		}
	}
	if err := validateRoutes(c.withDefaults()); err != nil {
		return err
	}
	if c.Files == nil {
		return fmt.Errorf("investor: Config.Files is nil: the signed contracts need a Disk, the application's own (filesystem.NewDisk), passed in here")
	}
	if c.Wallet == nil {
		return fmt.Errorf("investor: Config.Wallet is nil: arandu-investor needs arandu-wallet installed and registered in bootstrap/app.go; pass walletModule.Service() here")
	}
	if c.PageSize < 0 || c.PageSize > MaxPageSize {
		return fmt.Errorf("investor: Config.PageSize is %d, and has to be between 0 and %d, where 0 means %d", c.PageSize, MaxPageSize, DefaultPageSize)
	}
	return nil
}

// validateRoutes asks the standard library to parse the exact patterns the
// module will register, every one of them. Its parser is not exported and
// reports an invalid pattern, or two that collide, by panic, so the throwaway
// mux turns that boot-time panic into the configuration error New promises.
func validateRoutes(c Config) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("investor: Config.Prefix %q, Config.AdminPrefix %q and Config.Paths cannot be registered as routes: %v", c.Prefix, c.AdminPrefix, recovered)
		}
	}()

	handler := http.NotFoundHandler()
	mux := http.NewServeMux()
	for _, route := range routeTable(c) {
		mux.Handle(route.method+" "+route.path, handler)
	}
	return nil
}

// withDefaults returns the configuration with the optional fields filled in.
//
// It runs after Validate and never before: filling a default in first would
// hide the value somebody actually wrote from the check that would have refused
// it.
func (c Config) withDefaults() Config {
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	c.Paths = c.Paths.withDefaults()
	if c.AdminPrefix == "" {
		c.AdminPrefix = c.Prefix + c.Paths.Admin
	}
	if c.PageSize == 0 {
		c.PageSize = DefaultPageSize
	}
	if c.Location == nil {
		if loc, err := time.LoadLocation(DefaultLocation); err == nil {
			c.Location = loc
		} else {
			c.Location = time.FixedZone("BRT", -3*60*60)
		}
	}
	return c
}
