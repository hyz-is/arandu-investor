package investor

import (
	"fmt"
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Entry is one movement of the application's money that the investors'
// statements are made of: a sale, a renewal, a refund, a fee, an expense, a
// tax, an adjustment, or the reversal of one of them.
//
// Money is in centavos, signed: Gross is what the entry adds to the revenue
// (negative for a refund), Fee is what was taken from it on the way (the
// gateway, the invoice issuer, a tax). The net of the entry is Gross - Fee.
//
// An entry is never edited. A wrong one is undone by a reversal, a new entry
// that names it and carries the opposite amounts, so the statement of a closed
// period never changes after it was closed.
type Entry struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// ProductID is the application's product the money belongs to.
	ProductID string `db:"product_id"`

	// Kind is what the entry is.
	Kind EntryKind `db:"kind"`

	// OccurredAt is when it happened, and OccurredOn the calendar day of that
	// instant in the application's timezone -- the day the periods count by.
	OccurredAt time.Time `db:"occurred_at"`
	OccurredOn Day       `db:"occurred_on"`

	// Gross and Fee are centavos. Net is Gross - Fee, stored so a statement
	// sums one column.
	Gross int64 `db:"gross"`
	Fee   int64 `db:"fee"`
	Net   int64 `db:"net"`

	// FeeDetail says what the fee was: "gateway", "invoice", "withdrawal".
	FeeDetail string `db:"fee_detail"`

	Description string `db:"description"`

	// Source is where the entry came from: the application's code, or the
	// admin by hand.
	Source EntrySource `db:"source"`

	// ExternalRef is the caller's identifier of the event, unique per tenant:
	// sending the same event twice records it once.
	ExternalRef string `db:"external_ref"`

	// ReversesID is the entry a reversal undoes. Empty on every other kind.
	ReversesID string `db:"reverses_id"`

	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// EntryKind is what an entry is.
type EntryKind string

const (
	// KindSale is a first purchase.
	KindSale EntryKind = "sale"
	// KindRenewal is a recurring payment of something already sold.
	KindRenewal EntryKind = "renewal"
	// KindRefund is money given back to a buyer: negative revenue.
	KindRefund EntryKind = "refund"
	// KindFee is a charge taken on the way that is not tied to one sale.
	KindFee EntryKind = "fee"
	// KindExpense is money spent that is not one of the declared costs.
	KindExpense EntryKind = "expense"
	// KindTax is a tax paid.
	KindTax EntryKind = "tax"
	// KindAdjustment corrects the revenue by hand, either way.
	KindAdjustment EntryKind = "adjustment"
	// KindReversal undoes another entry.
	KindReversal EntryKind = "reversal"
)

// revenueKinds are the kinds whose Gross is revenue. The others carry their
// whole amount as Fee: what they cost, with no revenue of their own.
var revenueKinds = map[EntryKind]bool{
	KindSale: true, KindRenewal: true, KindRefund: true, KindAdjustment: true,
}

// EntrySource is where an entry came from.
type EntrySource string

const (
	// SourceAPI is the application's code, through RecordEntry.
	SourceAPI EntrySource = "api"
	// SourceManual is the admin, by hand.
	SourceManual EntrySource = "manual"
)

var entryTable = model.NewTable(model.TableSpec{
	Name:      entriesTable,
	New:       func() model.Entity { return new(Entry) },
	ManualKey: true,
})

// CostItem is one cost of running the application that is shared out over the
// revenue: a server, a cluster, a tool, the invoice issuer.
type CostItem struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	Name     string `db:"name"`
	Category string `db:"category"`

	// Amount is centavos per Recurrence: per month, per year, or once.
	Amount     int64      `db:"amount"`
	Recurrence Recurrence `db:"recurrence"`

	// StartsOn is the first day the cost runs, and the day a one-off cost
	// happens. EndsOn is the last day it runs, empty while it has no end.
	StartsOn Day `db:"starts_on"`
	EndsOn   Day `db:"ends_on"`

	// AllProducts is a cost of the whole application. When it is false, only
	// the products listed for it bear it.
	AllProducts Flag `db:"all_products"`

	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Recurrence is how often a cost's amount is due.
type Recurrence string

const (
	// Monthly is the amount every billing month.
	Monthly Recurrence = "monthly"
	// Yearly is the amount every year, a twelfth of it every billing month.
	Yearly Recurrence = "yearly"
	// Once is the amount on StartsOn, and never again.
	Once Recurrence = "once"
)

var costItemTable = model.NewTable(model.TableSpec{
	Name:      costItemsTable,
	New:       func() model.Entity { return new(CostItem) },
	ManualKey: true,
})

// CostItemProduct is one product that bears a cost that is not the whole
// application's.
type CostItemProduct struct {
	model.Model

	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	CostItemID string    `db:"cost_item_id"`
	ProductID  string    `db:"product_id"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

var costItemProductTable = model.NewTable(model.TableSpec{
	Name:      costItemProductsTable,
	New:       func() model.Entity { return new(CostItemProduct) },
	ManualKey: true,
})

// Invoice is what one closing of one contract produced: the period, the
// figures it was computed from, and the investor's amount. It is the unit the
// investor withdraws, and it is never summed into the next period.
type Invoice struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	InvestorID string `db:"investor_id"`
	// ContractID is the version in force when the period closed; RootID the
	// contract every version shares, which is what the periods follow.
	ContractID string `db:"contract_id"`
	RootID     string `db:"root_id"`

	// The period is [PeriodStart, PeriodEnd): the end day belongs to the next.
	PeriodStart Day `db:"period_start"`
	PeriodEnd   Day `db:"period_end"`

	// The figures, in centavos.
	Gross int64 `db:"gross"`
	Fees  int64 `db:"fees"`
	Costs int64 `db:"costs"`
	Net   int64 `db:"net"`

	// CostRatioPPM is the costs as a share of the gross, in parts per million:
	// what every line of the statement carries as its part of the costs.
	CostRatioPPM int64 `db:"cost_ratio_ppm"`

	ShareBps int64 `db:"share_bps"`
	Amount   int64 `db:"amount"`

	Status InvoiceStatus `db:"status"`

	// WalletOperationID is the deposit that put the amount in the investor's
	// wallet. Empty when there was nothing to deposit.
	WalletOperationID string `db:"wallet_operation_id"`

	// WithdrawalRequestID is the latest arandu-wallet withdrawal request made
	// for this invoice, and WithdrawalAttempts how many were made: a rejected
	// or cancelled one leaves the invoice to be asked for again.
	WithdrawalRequestID string `db:"withdrawal_request_id"`
	WithdrawalAttempts  int    `db:"withdrawal_attempts"`

	ClosedBy  string    `db:"closed_by"`
	ClosedAt  time.Time `db:"closed_at"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// InvoiceStatus is where an invoice stands.
type InvoiceStatus string

const (
	// InvoiceAvailable can be withdrawn.
	InvoiceAvailable InvoiceStatus = "available"
	// InvoiceOverdue can still be withdrawn; a later period has closed since.
	InvoiceOverdue InvoiceStatus = "overdue"
	// InvoiceWithdrawn was withdrawn.
	InvoiceWithdrawn InvoiceStatus = "withdrawn"
	// InvoiceNothingDue closed a period whose net was zero or negative: no
	// amount, no withdrawal, and no loss carried into the next period.
	InvoiceNothingDue InvoiceStatus = "nothing_due"
)

var invoiceTable = model.NewTable(model.TableSpec{
	Name:      invoicesTable,
	New:       func() model.Entity { return new(Invoice) },
	ManualKey: true,
})

// InvoiceEntry records that an entry was counted in an invoice of a contract.
// It is what lets an entry that arrived late -- dated in a period already
// closed -- be counted in the next closing instead of reopening the old one,
// and never twice.
type InvoiceEntry struct {
	model.Model

	ID        string    `db:"id"`
	TenantID  string    `db:"tenant_id"`
	InvoiceID string    `db:"invoice_id"`
	RootID    string    `db:"root_id"`
	EntryID   string    `db:"entry_id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var invoiceEntryTable = model.NewTable(model.TableSpec{
	Name:      invoiceEntriesTable,
	New:       func() model.Entity { return new(InvoiceEntry) },
	ManualKey: true,
})

// Reverse builds the reversal of an entry: the opposite amounts, the same
// product, pointing at it. The caller stamps the identifiers and the time.
func (e Entry) Reverse() (Entry, error) {
	if e.Kind == KindReversal {
		return Entry{}, fmt.Errorf("investor: entry %s is itself a reversal; record the original again instead", e.ID)
	}
	return Entry{
		ProductID:   e.ProductID,
		Kind:        KindReversal,
		Gross:       -e.Gross,
		Fee:         -e.Fee,
		Net:         -e.Net,
		FeeDetail:   e.FeeDetail,
		Description: "Estorno: " + e.Description,
		ReversesID:  e.ID,
	}, nil
}

// cents formats centavos as the decimal text arandu-wallet takes: 12345 is
// "123.45", -5 is "-0.05".
func cents(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}
