package investor

import (
	"context"
	"fmt"

	"github.com/arandu-io/framework/security"
)

// The actions of this package. Constants rather than strings at the call site:
// a typo in an action name would silently authorize nothing, or worse,
// everything.
//
// They carry the package in the name because an application registers many
// packages, and the name of an action shows up in logs and in audit trails
// where "view" on its own says nothing about what was viewed.
const (
	// InvestorsManage is registering, changing and suspending investors, and
	// keeping their contracts.
	InvestorsManage security.Action = "investor.investors.manage"
	// EntriesManage is recording entries by hand, reversing them and closing a
	// contract's period.
	EntriesManage security.Action = "investor.entries.manage"
	// CostsManage is declaring the costs of the system.
	CostsManage security.Action = "investor.costs.manage"
	// DashboardView is reading an investor's statement, costs and projection.
	DashboardView security.Action = "investor.dashboard.view"
	// ContractView is reading and downloading an investor's own contract.
	ContractView security.Action = "investor.contract.view"
	// ReportsView is reading an investor's invoices and income reports.
	ReportsView security.Action = "investor.reports.view"
	// AdminAction is being this package's admin, held as an action: what an
	// application whose permissions arandu-permission administers puts in a
	// group, so the people of that group are the admin without a role
	// written on their accounts. It opens what AdminRole opens, and nothing
	// more.
	AdminAction security.Action = "investor.admin"
)

// IsAdmin says whether the subject is this package's admin: they carry
// AdminRole, or AdminAction among the actions arandu-permission resolved for
// them. The policy decides with it; an application asks it to decide what to
// offer, never to authorize.
func IsAdmin(s security.Subject) bool { return s.HasRole(AdminRole) || s.Can(AdminAction) }

// AdminRole is the role an application grants to the people who run the
// investors' money: finance, whoever registers investors, closes periods and
// declares costs.
//
// One role and not several. The rules below need exactly one distinction --
// the investor, and somebody acting across the tenant on the investors'
// behalf. Which people hold it is the application's to decide, through
// arandu-permission; this package only reads it off the subject.
const AdminRole = "investor.admin"

// InvestorPolicy is the only authority over who does what with an Investor
// and everything that belongs to one: contracts, statement, invoices.
//
// It denies unless a rule below says otherwise, and the rules are written
// around two subjects: the investor, who may read what is theirs, and the
// admin, who may act across the tenant. Everything else -- a guest, a subject
// from another tenant, a signed-in person reaching for somebody else's
// statement -- falls through to the refusal at the end.
type InvestorPolicy struct{}

// Compile-time proof that the policy answers about this entity and no other. A
// policy that drifted onto another type would leave this one unguarded while
// the Model path still compiled.
var _ security.Policy[Investor] = InvestorPolicy{}

// Can decides whether the subject may perform the action on the record.
//
// It is the only place that decides. The service reaches the Model only after
// Authorize turns this method's nil result into a Grant.
//
// The record is the empty Investor where the question is "may this subject do
// this kind of thing at all", and the loaded row where it is "may they do it to
// this investor". Both are asked, in that order, and the second is what a rule
// about ownership answers.
func (InvestorPolicy) Can(ctx context.Context, s security.Subject, a security.Action, record Investor) error {
	// Tenant isolation comes first and applies to every action. Without it every
	// check below would be pointless in a multi-tenant system: a rule that
	// allows an owner to read their own record would allow it across customers
	// as soon as two of them have a record with the same identifier.
	//
	// The empty id is the candidate that has not been stored yet, which belongs
	// to nobody until it is written with the tenant off the Grant.
	if record.ID != "" && record.TenantID != s.Tenant {
		return fmt.Errorf("investor belongs to another tenant")
	}

	// arandu:begin custom
	// There is no money a visitor with no session owns, and saying so here
	// keeps the reason visible instead of leaving it to the fall-through.
	if s.IsGuest() {
		return fmt.Errorf("a guest is no investor")
	}

	// The admin acts across the tenant. The tenant check above already ran, so
	// this is wide inside one customer and reaches no further. The actions are
	// enumerated rather than allowed wholesale, so an action somebody adds later
	// is refused to everybody until a rule names it.
	if IsAdmin(s) {
		switch a {
		case InvestorsManage, EntriesManage, CostsManage, DashboardView, ContractView, ReportsView:
			return nil
		}
	}

	// What an investor reads about themselves. The probe -- the empty record --
	// asks whether this kind of reading is possible at all, and the service
	// then narrows the statement to the investor whose user is the subject. The
	// second call, on the loaded row, is where the ownership is decided.
	switch a {
	case DashboardView, ContractView, ReportsView:
		if record.ID == "" {
			return nil
		}
		if record.UserID != "" && record.UserID == s.ID {
			return nil
		}
	}
	// arandu:end custom

	return fmt.Errorf("no rule allows %s on investor", a)
}
