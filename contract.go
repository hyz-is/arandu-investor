package investor

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Contract is what an investor signed: the share of the net they earn, the day
// the period closes, the products whose revenue counts, and the signed file.
//
// An amendment is a new Contract with the same RootID as the original and the
// next Version. Nothing is overwritten: the old version stays readable,
// superseded, and the closings already made against it keep pointing at it.
type Contract struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// InvestorID is whose contract this is.
	InvestorID string `db:"investor_id"`

	// Number is how the contract is called on paper.
	Number string `db:"number"`

	// Version is 1 for the original and grows by one with each amendment.
	Version int `db:"version"`

	// RootID is the original contract every version belongs to: the
	// original's own id on the original, and the same id on each amendment.
	RootID string `db:"root_id"`

	// SignedOn, StartsOn and EndsOn are calendar days. EndsOn is empty while
	// the contract has no end.
	SignedOn Day `db:"signed_on"`
	StartsOn Day `db:"starts_on"`
	EndsOn   Day `db:"ends_on"`

	// ShareBps is the investor's share of the net, in basis points: 1500 is 15%.
	ShareBps int64 `db:"share_bps"`

	// BillingDay is the day of the month the contract's period closes, 1 to 28.
	BillingDay int `db:"billing_day"`

	// AllProducts is a contract over the whole application. When it is false,
	// only the products listed for it count.
	AllProducts Flag `db:"all_products"`

	// Status is whether this version is the one in force.
	Status ContractStatus `db:"status"`

	// The signed file, kept on the Disk the application passed in. FileKey is
	// the key on that Disk; the tenant prefix is the Disk's, from the Grant.
	FileKey    string `db:"file_key"`
	FileName   string `db:"file_name"`
	FileType   string `db:"file_type"`
	FileSize   int64  `db:"file_size"`
	FileSHA256 string `db:"file_sha256"`

	// CreatedBy is the subject who registered this version.
	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// ContractStatus is whether a contract version is the one in force.
type ContractStatus string

const (
	// ContractActive is the version in force.
	ContractActive ContractStatus = "active"
	// ContractSuperseded was replaced by an amendment.
	ContractSuperseded ContractStatus = "superseded"
)

var contractTable = model.NewTable(model.TableSpec{
	Name:      contractsTable,
	New:       func() model.Entity { return new(Contract) },
	ManualKey: true,
})

// ContractProduct is one product whose revenue counts for a contract that does
// not cover the whole application.
type ContractProduct struct {
	model.Model

	ID         string `db:"id"`
	TenantID   string `db:"tenant_id"`
	ContractID string `db:"contract_id"`
	// ProductID is the application's identifier of the product. This package
	// never resolves it: what a product is belongs to the application.
	ProductID string    `db:"product_id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var contractProductTable = model.NewTable(model.TableSpec{
	Name:      contractProductsTable,
	New:       func() model.Entity { return new(ContractProduct) },
	ManualKey: true,
})

// InForce reports whether the contract covers the day.
func (c Contract) InForce(day Day) bool {
	if c.Status != ContractActive || day.Before(c.StartsOn) {
		return false
	}
	return c.EndsOn.IsZero() || !c.EndsOn.Before(day)
}

// Supersede marks this version as replaced by an amendment.
func (c *Contract) Supersede() error {
	if c.Status != ContractActive {
		return fmt.Errorf("investor: contract %s version %d is not the one in force", c.Number, c.Version)
	}
	c.Status = ContractSuperseded
	return nil
}

// Day is a calendar day, stored as YYYY-MM-DD.
//
// A day and not a timestamp, because a contract starts on the 20th in the
// customer's calendar, not at an instant in some timezone, and a timestamp
// column turns that into a different day on every server that reads it in
// another zone. The empty Day is "no day", which is how an open end is kept.
type Day string

// DayLayout is the format a Day is written in.
const DayLayout = "2006-01-02"

// ParseDay reads a day, refusing anything that is not a real date.
func ParseDay(s string) (Day, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(DayLayout, s)
	if err != nil {
		return "", fmt.Errorf("investor: %q is not a day in the form %s", s, DayLayout)
	}
	return Day(t.Format(DayLayout)), nil
}

// DayOf is the calendar day of an instant, in the location given.
func DayOf(t time.Time, loc *time.Location) Day {
	return Day(t.In(loc).Format(DayLayout))
}

// IsZero reports whether the day is unset.
func (d Day) IsZero() bool { return d == "" }

// Time is the day at midnight UTC, the one instant used for arithmetic.
func (d Day) Time() time.Time {
	t, _ := time.Parse(DayLayout, string(d))
	return t
}

// Before reports whether d is earlier than other. An unset day is never
// before anything.
func (d Day) Before(other Day) bool {
	if d.IsZero() || other.IsZero() {
		return false
	}
	return string(d) < string(other)
}

// AddDays moves the day by n days.
func (d Day) AddDays(n int) Day { return Day(d.Time().AddDate(0, 0, n).Format(DayLayout)) }

// DaysUntil is the number of days from d to other, negative when other is
// earlier.
func (d Day) DaysUntil(other Day) int {
	return int(other.Time().Sub(d.Time()).Hours() / 24)
}

// Flag is a yes-or-no a column holds as an integer, read back from whatever an
// engine answers with -- the same arrangement arandu-wallet keeps, because the
// engines disagree about booleans and one place has to flatten them.
type Flag bool

// Value writes the flag as the integer the column holds.
func (f Flag) Value() (driver.Value, error) {
	if f {
		return int64(1), nil
	}
	return int64(0), nil
}

// Scan reads the flag back. Anything it cannot read is refused rather than
// read as false.
func (f *Flag) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*f = false
	case bool:
		*f = Flag(v)
	case int64:
		*f = v != 0
	case int32:
		*f = v != 0
	case int:
		*f = v != 0
	case float64:
		*f = v != 0
	case []byte:
		return f.Scan(string(v))
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("investor: %q is not a yes-or-no", v)
		}
		*f = Flag(b)
	default:
		return fmt.Errorf("investor: cannot read %T as a yes-or-no", value)
	}
	return nil
}

// ContractResource is the list of fields one Contract answers with.
type ContractResource struct {
	c        Contract
	products []string
}

// NewContractResource snapshots one contract and its products for a response.
func NewContractResource(c Contract, products []string) ContractResource {
	return ContractResource{c: c, products: products}
}

// Contract is the version the resource describes.
func (r ContractResource) Contract() Contract { return r.c }

// Products are the products the version's revenue counts, empty when it
// counts the whole application's.
func (r ContractResource) Products() []string { return append([]string(nil), r.products...) }

// ToArray returns the fields that may leave, by name. The file is described,
// never linked: it is read through the package's own download route, which
// asks the policy first.
func (r ContractResource) ToArray() map[string]any {
	products := r.products
	if products == nil {
		products = []string{}
	}
	return map[string]any{
		"id":           r.c.ID,
		"investor_id":  r.c.InvestorID,
		"number":       r.c.Number,
		"version":      r.c.Version,
		"root_id":      r.c.RootID,
		"signed_on":    string(r.c.SignedOn),
		"starts_on":    string(r.c.StartsOn),
		"ends_on":      string(r.c.EndsOn),
		"share_bps":    r.c.ShareBps,
		"billing_day":  r.c.BillingDay,
		"all_products": bool(r.c.AllProducts),
		"products":     products,
		"status":       string(r.c.Status),
		"file_name":    r.c.FileName,
		"file_size":    r.c.FileSize,
		"created_at":   r.c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// With returns nothing beside the fields.
func (r ContractResource) With() map[string]any { return nil }

// ContractList is a list of contracts as one response.
type ContractList struct{ items []ContractResource }

// ToArray returns the list under a single key.
func (c ContractList) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(c.items))
	for _, item := range c.items {
		items = append(items, item.ToArray())
	}
	return map[string]any{"items": items}
}

// With returns nothing beside the list.
func (c ContractList) With() map[string]any { return nil }
