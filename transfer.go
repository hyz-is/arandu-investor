package investor

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// InvestorTransfer is one change of who an investor is: the registration kept
// its contracts, its entries and its invoices, and passed to another person.
// The row is the history of the change, written when it happened and never
// edited, so the statements of the earlier owner can still be told who they
// were written for.
//
// The From fields are the registration as it stood before the change: the
// login user, the kind, the document and the names. The investor row itself
// always holds the current owner, so the owner between two transfers is the
// From of the later one.
//
// Build a new row with InvestorTransfers(db).New(): a struct literal has no
// connection and its write methods return model.ErrUnwired.
type InvestorTransfer struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// InvestorID is the registration that changed hands.
	InvestorID string `db:"investor_id"`

	// FromUserID, FromKind, FromDocument, FromName, FromLegalName,
	// FromLegalRepresentative and FromEmail are the registration before.
	FromUserID              string     `db:"from_user_id"`
	FromKind                PersonKind `db:"from_kind"`
	FromDocument            string     `db:"from_document"`
	FromName                string     `db:"from_name"`
	FromLegalName           string     `db:"from_legal_name"`
	FromLegalRepresentative string     `db:"from_legal_representative"`
	FromEmail               string     `db:"from_email"`

	// ToUserID and ToName say who the registration passed to.
	ToUserID string `db:"to_user_id"`
	ToName   string `db:"to_name"`

	// EffectiveOn is the calendar day the new owner takes over, in the
	// application's timezone.
	EffectiveOn Day `db:"effective_on"`

	// Reason is why, in the words of whoever did it.
	Reason string `db:"reason"`

	// CreatedBy is the subject who made the change.
	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var investorTransferTable = model.NewTable(model.TableSpec{
	Name:      investorTransfersTable,
	New:       func() model.Entity { return new(InvestorTransfer) },
	ManualKey: true,
})
