package investor

import (
	stdhttp "net/http"
)

// URLs are the addresses of this package's routes, built from Config.Prefix,
// Config.AdminPrefix and Config.Paths. The route table is built from the same
// methods with the wildcard in place of the identifier, so an address a screen
// links to and the pattern that answers it are one spelling.
type URLs struct {
	prefix, admin string
	p             Paths
}

// URLs answers the addresses the module is mounted at, for an application that
// links to them from its own pages.
func (m *Module) URLs() URLs { return urlsOf(m.cfg) }

// NewURLs builds the addresses for a prefix, an admin prefix and the paths, as
// New would from a Config holding them. Empty segments take DefaultPaths, and
// an empty admin prefix is the prefix followed by Paths.Admin.
func NewURLs(prefix, adminPrefix string, paths Paths) URLs {
	paths = paths.withDefaults()
	if adminPrefix == "" {
		adminPrefix = prefix + paths.Admin
	}
	return URLs{prefix: prefix, admin: adminPrefix, p: paths}
}

func urlsOf(c Config) URLs { return URLs{prefix: c.Prefix, admin: c.AdminPrefix, p: c.Paths} }

func join(base, segment string) string {
	if segment == "/" {
		return base
	}
	return base + segment
}

// Prefix is where the investor's routes are mounted.
func (u URLs) Prefix() string { return u.prefix }

// AdminPrefix is where the admin's routes are mounted.
func (u URLs) AdminPrefix() string { return u.admin }

// Me is the investor's own panel.
func (u URLs) Me() string { return join(u.prefix, u.p.Me) }

// MyContracts lists the investor's own contracts.
func (u URLs) MyContracts() string { return join(u.Me(), u.p.Contracts) }

// MyInvoices lists the investor's own invoices.
func (u URLs) MyInvoices() string { return join(u.Me(), u.p.Invoices) }

// MyReports is the base of the investor's reports: /{year} and /{year}/{month}
// below it.
func (u URLs) MyReports() string { return join(u.Me(), u.p.Reports) }

// PayoutAccounts is where the investor declares an account to be paid into.
func (u URLs) PayoutAccounts() string { return join(u.Me(), u.p.PayoutAccounts) }

// Contract is the base of one contract's routes.
func (u URLs) contract(id string) string { return u.prefix + u.p.Contracts + "/" + id }

// ContractFile downloads a contract's signed copy.
func (u URLs) ContractFile(id string) string { return u.contract(id) + u.p.File }

// Statement is a contract's statement.
func (u URLs) Statement(id string) string { return u.contract(id) + u.p.Statement }

// Projection is a contract's open period.
func (u URLs) Projection(id string) string { return u.contract(id) + u.p.Projection }

// Costs are the declared costs, as the investor reads them.
func (u URLs) Costs() string { return u.prefix + u.p.Costs }

// Withdraw asks for one invoice, whole.
func (u URLs) Withdraw(invoiceID string) string {
	return u.prefix + u.p.Invoices + "/" + invoiceID + u.p.Withdrawal
}

// Receipt downloads the receipt of an invoice's paid withdrawal.
func (u URLs) Receipt(invoiceID string) string {
	return u.prefix + u.p.Invoices + "/" + invoiceID + u.p.Receipt
}

// Investors is the admin's registry.
func (u URLs) Investors() string { return u.admin + u.p.Investors }

// Investor is the admin's page of one investor.
func (u URLs) Investor(id string) string { return u.Investors() + "/" + id }

// Suspension suspends (POST) or reactivates (DELETE) an investor.
func (u URLs) Suspension(id string) string { return u.Investor(id) + u.p.Suspension }

// InvestorContracts lists (GET) and registers (POST) an investor's contracts.
func (u URLs) InvestorContracts(id string) string { return u.Investor(id) + u.p.Contracts }

// InvestorInvoices lists an investor's invoices for the admin.
func (u URLs) InvestorInvoices(id string) string { return u.Investor(id) + u.p.Invoices }

// InvestorReports is the base of an investor's reports for the admin.
func (u URLs) InvestorReports(id string) string { return u.Investor(id) + u.p.Reports }

// Amend registers an amendment of a contract.
func (u URLs) Amend(contractID string) string {
	return u.admin + u.p.Contracts + "/" + contractID + u.p.Amendments
}

// Close closes a contract's period.
func (u URLs) Close(contractID string) string {
	return u.admin + u.p.Contracts + "/" + contractID + u.p.Closings
}

// CloseDue closes every contract due on a day.
func (u URLs) CloseDue() string { return u.admin + u.p.Closings + u.p.Due }

// Entries are the admin's books.
func (u URLs) Entries() string { return u.admin + u.p.Entries }

// Reverse reverses one entry.
func (u URLs) Reverse(id string) string { return u.Entries() + "/" + id + u.p.Reversal }

// AdminCosts are the declared costs, for the admin who declares them.
func (u URLs) AdminCosts() string { return u.admin + u.p.Costs }

// EndCost ends a declared cost.
func (u URLs) EndCost(id string) string { return u.AdminCosts() + "/" + id + u.p.End }

// Withdrawals is the queue of investors' withdrawals.
func (u URLs) Withdrawals() string { return u.admin + u.p.Withdrawals }

// Process takes a withdrawal to be paid.
func (u URLs) Process(id string) string { return u.Withdrawals() + "/" + id + u.p.Processing }

// Pay records a withdrawal paid, with its receipt.
func (u URLs) Pay(id string) string { return u.Withdrawals() + "/" + id + u.p.Payment }

// Reject refuses a withdrawal with a reason.
func (u URLs) Reject(id string) string { return u.Withdrawals() + "/" + id + u.p.Rejection }

// route is one entry of the table Routes registers and Validate parses.
type route struct {
	method, path, name string
	admin              bool
}

// routeTable is every route of the package, by the name it is registered
// under. The names never change with the paths: a URL is built from a name,
// and a name that moved with the language would be a 404 in a template
// somebody else wrote.
func routeTable(c Config) []route {
	u := urlsOf(c)
	const id = "{id}"
	get, post, put, del := stdhttp.MethodGet, stdhttp.MethodPost, stdhttp.MethodPut, stdhttp.MethodDelete
	return []route{
		// The investor's own side.
		{get, u.Me(), "investor.me", false},
		{get, u.MyContracts(), "investor.me.contracts", false},
		{get, u.ContractFile(id), "investor.contracts.file", false},
		{get, u.Statement(id), "investor.contracts.statement", false},
		{get, u.Projection(id), "investor.contracts.projection", false},
		{get, u.Costs(), "investor.costs.index", false},
		{get, u.MyInvoices(), "investor.me.invoices", false},
		{post, u.Withdraw(id), "investor.invoices.withdraw", false},
		{get, u.MyReports() + "/{year}/{month}", "investor.me.reports.monthly", false},
		{get, u.MyReports() + "/{year}", "investor.me.reports.annual", false},
		{post, u.PayoutAccounts(), "investor.me.payout_accounts.store", false},
		{get, u.Receipt(id), "investor.invoices.receipt", false},

		// The admin's side.
		{get, u.Investors(), "investor.admin.investors.index", true},
		{post, u.Investors(), "investor.admin.investors.store", true},
		{get, u.Investor(id), "investor.admin.investors.show", true},
		{put, u.Investor(id), "investor.admin.investors.update", true},
		{post, u.Suspension(id), "investor.admin.investors.suspend", true},
		{del, u.Suspension(id), "investor.admin.investors.reactivate", true},
		{get, u.InvestorContracts(id), "investor.admin.contracts.index", true},
		{post, u.InvestorContracts(id), "investor.admin.contracts.store", true},
		{post, u.Amend(id), "investor.admin.contracts.amend", true},
		{get, u.Entries(), "investor.admin.entries.index", true},
		{post, u.Entries(), "investor.admin.entries.store", true},
		{post, u.Reverse(id), "investor.admin.entries.reverse", true},
		{get, u.AdminCosts(), "investor.admin.costs.index", true},
		{post, u.AdminCosts(), "investor.admin.costs.store", true},
		{post, u.EndCost(id), "investor.admin.costs.end", true},
		{post, u.Close(id), "investor.admin.closings.store", true},
		{post, u.CloseDue(), "investor.admin.closings.due", true},
		{get, u.InvestorInvoices(id), "investor.admin.invoices.index", true},
		{get, u.InvestorReports(id) + "/{year}/{month}", "investor.admin.reports.monthly", true},
		{get, u.InvestorReports(id) + "/{year}", "investor.admin.reports.annual", true},
		{get, u.Withdrawals(), "investor.admin.withdrawals.index", true},
		{post, u.Process(id), "investor.admin.withdrawals.process", true},
		{post, u.Pay(id), "investor.admin.withdrawals.pay", true},
		{post, u.Reject(id), "investor.admin.withdrawals.reject", true},
	}
}
