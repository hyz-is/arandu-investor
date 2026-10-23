package investor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
)

// EntryRequest is one event the application reports: a sale, a renewal, a
// refund, a fee, an expense or a tax. It is what the application's own events
// endpoint -- which it owns, with its own token per tenant -- turns its body
// into before calling RecordEntry.
type EntryRequest struct {
	// ExternalRef identifies the event in the caller's system. Sending the same
	// one twice records it once and answers the entry already recorded.
	ExternalRef string
	Kind        EntryKind
	ProductID   string
	OccurredAt  time.Time
	// Gross is centavos of revenue: positive for a sale, a renewal or an
	// adjustment up, negative for a refund or an adjustment down. Zero for a
	// fee, an expense or a tax.
	Gross int64
	// Fee is centavos taken on the way: the gateway's cut of a sale, or the
	// whole amount of a fee, an expense or a tax.
	Fee         int64
	FeeDetail   string
	Description string
}

// Validate reports the errors per field.
func (r EntryRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "external_ref", r.ExternalRef)
	validation.MaxLen(e, "external_ref", r.ExternalRef, maxNameLen)
	validation.Required(e, "product_id", r.ProductID)
	validation.MaxLen(e, "product_id", r.ProductID, maxIdentifierLen)
	validation.MaxLen(e, "fee_detail", r.FeeDetail, maxShortLen)
	validation.MaxLen(e, "description", r.Description, maxTextLen)
	if r.OccurredAt.IsZero() {
		e.Add("occurred_at", "is required")
	}
	if r.Fee < 0 {
		e.Add("fee", "cannot be negative")
	}
	switch r.Kind {
	case KindSale, KindRenewal:
		if r.Gross <= 0 {
			e.Add("gross", "must be positive for a sale or a renewal")
		}
	case KindRefund:
		if r.Gross >= 0 {
			e.Add("gross", "must be negative for a refund")
		}
	case KindAdjustment:
		if r.Gross == 0 && r.Fee == 0 {
			e.Add("gross", "an adjustment has to change something")
		}
	case KindFee, KindExpense, KindTax:
		if r.Gross != 0 {
			e.Add("gross", "must be zero: a "+string(r.Kind)+" is carried as its fee")
		}
		if r.Fee <= 0 {
			e.Add("fee", "must be positive for a "+string(r.Kind))
		}
	default:
		e.Add("kind", "must be sale, renewal, refund, fee, expense, tax or adjustment")
	}
	return e
}

var _ validation.Validatable = EntryRequest{}

// RecordEntry records one event, once.
//
// It is idempotent by ExternalRef within the tenant: a second call with the
// same reference answers the entry the first one recorded, whatever the rest of
// the body says, because the caller retrying is the case this exists for. The
// unique index is what decides between two concurrent first calls.
func (s *InvestorService) RecordEntry(ctx context.Context, actor security.Subject, in EntryRequest) (*Entry, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(in.ExternalRef)
	if existing, err := Entries(s.db).Where("external_ref", "=", ref).First(ctx, g); err != nil || existing != nil {
		return existing, err
	}
	return s.insertEntry(ctx, actor, g, in, SourceAPI)
}

// RecordManualEntry is RecordEntry for the admin's own hand: the same rules,
// marked as manual in the statement so the investor sees which lines a person
// typed.
func (s *InvestorService) RecordManualEntry(ctx context.Context, actor security.Subject, in EntryRequest) (*Entry, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(in.ExternalRef)
	if existing, err := Entries(s.db).Where("external_ref", "=", ref).First(ctx, g); err != nil || existing != nil {
		return existing, err
	}
	entry, err := s.insertEntry(ctx, actor, g, in, SourceManual)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventEntryRecorded, "investor_entry", entry.ID, map[string]any{"kind": string(entry.Kind), "gross": entry.Gross, "fee": entry.Fee, "external_ref": entry.ExternalRef}); err != nil {
		return nil, err
	}
	return entry, nil
}

// insertEntry writes a validated, authorized entry the caller found was not
// recorded yet. The unique index decides between two concurrent first calls.
func (s *InvestorService) insertEntry(ctx context.Context, actor security.Subject, g security.Grant, in EntryRequest, source EntrySource) (*Entry, error) {
	ref := strings.TrimSpace(in.ExternalRef)
	record, err := Entries(s.db).New()
	if err != nil {
		return nil, err
	}
	if record.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	record.TenantID = data.Tenant(g)
	record.ProductID = strings.TrimSpace(in.ProductID)
	record.Kind = in.Kind
	record.OccurredAt = in.OccurredAt.UTC()
	record.OccurredOn = DayOf(in.OccurredAt, s.location())
	record.Gross = in.Gross
	record.Fee = in.Fee
	record.Net = in.Gross - in.Fee
	record.FeeDetail = strings.TrimSpace(in.FeeDetail)
	record.Description = strings.TrimSpace(in.Description)
	record.Source = source
	record.ExternalRef = ref
	record.CreatedBy = actor.ID
	record.CreatedAt, record.UpdatedAt = now, now
	if _, err := record.Save(ctx, g); err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			// A concurrent first call won; answer what it recorded.
			return Entries(s.db).Where("external_ref", "=", ref).First(ctx, g)
		}
		return nil, err
	}
	return record, nil
}

// ReverseEntry undoes an entry with a new one carrying the opposite amounts,
// dated now: an entry of a period already closed is corrected in the period
// that is open, never by reopening the old one. Reversing the same entry twice
// answers the first reversal.
func (s *InvestorService) ReverseEntry(ctx context.Context, actor security.Subject, entryID, reason string) (*Entry, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	original, err := Entries(s.db).WhereKey(entryID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrNotFound
	}
	reversal, err := original.Reverse()
	if err != nil {
		return nil, validation.Errors{"entry": {err.Error()}}
	}
	ref := "reversal:" + original.ID
	if existing, err := Entries(s.db).Where("external_ref", "=", ref).First(ctx, g); err != nil || existing != nil {
		return existing, err
	}
	record, err := Entries(s.db).New()
	if err != nil {
		return nil, err
	}
	if record.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	record.TenantID = data.Tenant(g)
	record.ProductID = reversal.ProductID
	record.Kind = KindReversal
	record.OccurredAt = now
	record.OccurredOn = DayOf(now, s.location())
	record.Gross, record.Fee, record.Net = reversal.Gross, reversal.Fee, reversal.Net
	record.FeeDetail = reversal.FeeDetail
	record.Description = reversal.Description
	if reason = strings.TrimSpace(reason); reason != "" {
		record.Description += " (" + reason + ")"
	}
	record.Source = SourceManual
	record.ExternalRef = ref
	record.ReversesID = original.ID
	record.CreatedBy = actor.ID
	record.CreatedAt, record.UpdatedAt = now, now
	if _, err := record.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventEntryReversed, "investor_entry", original.ID, map[string]any{"reversal_id": record.ID, "reason": strings.TrimSpace(reason)}); err != nil {
		return nil, err
	}
	return record, nil
}

// CostRequest declares one cost of running the application.
type CostRequest struct {
	Name        string
	Category    string
	Amount      int64
	Recurrence  Recurrence
	StartsOn    string
	EndsOn      string
	AllProducts bool
	Products    []string
}

// Validate reports the errors per field.
func (r CostRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "name", r.Name)
	validation.MaxLen(e, "name", r.Name, maxNameLen)
	validation.Required(e, "category", r.Category)
	validation.MaxLen(e, "category", r.Category, maxShortLen)
	if r.Amount <= 0 {
		e.Add("amount", "must be positive, in centavos")
	}
	switch r.Recurrence {
	case Monthly, Yearly, Once:
	default:
		e.Add("recurrence", "must be monthly, yearly or once")
	}
	starts, err := ParseDay(r.StartsOn)
	if err != nil || starts.IsZero() {
		e.Add("starts_on", "must be a day in the form "+DayLayout)
	}
	ends, err := ParseDay(r.EndsOn)
	if err != nil {
		e.Add("ends_on", "must be a day in the form "+DayLayout+", or empty")
	} else if !ends.IsZero() && ends.Before(starts) {
		e.Add("ends_on", "must not be before the start")
	}
	if !r.AllProducts && len(cleanProducts(r.Products)) == 0 {
		e.Add("products", "name at least one product, or charge the whole application")
	}
	return e
}

var _ validation.Validatable = CostRequest{}

// CreateCost declares one cost.
func (s *InvestorService) CreateCost(ctx context.Context, actor security.Subject, in CostRequest) (*CostItem, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, CostsManage, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := CostItems(s.db).New()
	if err != nil {
		return nil, err
	}
	if record.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	record.TenantID = data.Tenant(g)
	record.Name = strings.TrimSpace(in.Name)
	record.Category = strings.TrimSpace(in.Category)
	record.Amount = in.Amount
	record.Recurrence = in.Recurrence
	record.StartsOn, _ = ParseDay(in.StartsOn)
	record.EndsOn, _ = ParseDay(in.EndsOn)
	record.AllProducts = Flag(in.AllProducts)
	record.CreatedBy = actor.ID
	record.CreatedAt, record.UpdatedAt = now, now

	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if _, err := record.Save(ctx, g); err != nil {
			return err
		}
		if record.AllProducts {
			return nil
		}
		for _, product := range cleanProducts(in.Products) {
			link, err := CostItemProducts(s.db).New()
			if err != nil {
				return err
			}
			if link.ID, err = data.NewID(); err != nil {
				return err
			}
			link.TenantID = data.Tenant(g)
			link.CostItemID = record.ID
			link.ProductID = product
			link.CreatedAt, link.UpdatedAt = now, now
			if _, err := link.Save(ctx, g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventCostCreated, "investor_cost", record.ID, map[string]any{"name": record.Name, "amount": record.Amount, "recurrence": string(record.Recurrence)}); err != nil {
		return nil, err
	}
	return record, nil
}

// EndCost stops a cost from the day after the one given. The months it ran
// stay counted; nothing about a closed period changes.
func (s *InvestorService) EndCost(ctx context.Context, actor security.Subject, costID, lastDay string) (*CostItem, error) {
	ends, err := ParseDay(lastDay)
	if err != nil || ends.IsZero() {
		return nil, validation.Errors{"ends_on": {"must be a day in the form " + DayLayout}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, CostsManage, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := CostItems(s.db).WhereKey(costID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if ends.Before(record.StartsOn) {
		return nil, validation.Errors{"ends_on": {"must not be before the start"}}
	}
	record.EndsOn = ends
	record.UpdatedAt = s.now()
	if _, err := record.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventCostEnded, "investor_cost", record.ID, map[string]any{"ends_on": string(record.EndsOn)}); err != nil {
		return nil, err
	}
	return record, nil
}

// CostView is one cost with the products that bear it.
type CostView struct {
	Item     CostItem
	Products []string
}

// Costs lists the declared costs, for the admin and for the investors: what
// the money is spent on is part of what an investor is shown.
func (s *InvestorService) Costs(ctx context.Context, actor security.Subject) ([]CostView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Investor{})
	if err != nil {
		return nil, err
	}
	// The probe for DashboardView answers yes to anybody signed in; the costs
	// are shown to the admin and to the investors, so somebody who is neither
	// is refused on their row.
	if !actor.HasRole(AdminRole) {
		me, err := Investors(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
		if err != nil {
			return nil, err
		}
		if me == nil {
			return nil, fmt.Errorf("investor: %s is not an investor of this tenant: %w", actor.ID, security.ErrForbidden)
		}
		if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
			return nil, err
		}
	}
	return s.loadCosts(ctx, g)
}

// loadCosts reads every cost and its products, after the caller authorized.
func (s *InvestorService) loadCosts(ctx context.Context, g security.Grant) ([]CostView, error) {
	items, err := CostItems(s.db).OrderBy("starts_on").OrderBy("id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	links, err := CostItemProducts(s.db).OrderBy("product_id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	byItem := map[string][]string{}
	for _, l := range links {
		byItem[l.CostItemID] = append(byItem[l.CostItemID], l.ProductID)
	}
	out := make([]CostView, 0, len(items))
	for _, item := range items {
		out = append(out, CostView{Item: *item, Products: byItem[item.ID]})
	}
	return out, nil
}

// scopeOf is the scope of a cost as the calculation takes it.
func (c CostView) scopeOf() Scope {
	if c.Item.AllProducts {
		return Scope{All: true}
	}
	set := map[string]bool{}
	for _, p := range c.Products {
		set[p] = true
	}
	return Scope{Products: set}
}
