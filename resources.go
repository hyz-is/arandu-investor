package investor

import (
	"time"
)

// The responses of the books: declared lists of fields, never the entities,
// so a column added later does not leave through a response nobody reopened --
// and the tenant never leaves at all.

func entryFields(e Entry) map[string]any {
	return map[string]any{
		"id":           e.ID,
		"product_id":   e.ProductID,
		"kind":         string(e.Kind),
		"occurred_at":  e.OccurredAt.UTC().Format(time.RFC3339),
		"occurred_on":  string(e.OccurredOn),
		"gross":        e.Gross,
		"fee":          e.Fee,
		"net":          e.Net,
		"fee_detail":   e.FeeDetail,
		"description":  e.Description,
		"source":       string(e.Source),
		"external_ref": e.ExternalRef,
		"reverses_id":  e.ReversesID,
	}
}

type entryResource struct{ e Entry }

func (r entryResource) ToArray() map[string]any { return entryFields(r.e) }
func (r entryResource) With() map[string]any    { return nil }

type statementResource struct{ s *Statement }

func (r statementResource) ToArray() map[string]any {
	lines := make([]map[string]any, 0, len(r.s.Lines))
	for _, l := range r.s.Lines {
		row := entryFields(l.Entry)
		row["cost"] = l.Cost
		row["line_net"] = l.Net
		row["estimate"] = r.s.Open[l.Entry.ID]
		lines = append(lines, row)
	}
	return map[string]any{
		"contract_id": r.s.Contract.ID,
		"lines":       lines,
		"totals": map[string]any{
			"gross": r.s.Gross, "fees": r.s.Fees, "costs": r.s.Costs, "net": r.s.Net,
		},
	}
}
func (r statementResource) With() map[string]any { return nil }

type projectionResource struct{ p *Projection }

func (r projectionResource) ToArray() map[string]any {
	costs := make([]map[string]any, 0, len(r.p.Costs))
	for _, c := range r.p.Costs {
		products := c.Products
		if products == nil {
			products = []string{}
		}
		costs = append(costs, map[string]any{
			"id": c.Item.ID, "name": c.Item.Name, "category": c.Item.Category,
			"recurrence": string(c.Item.Recurrence), "all_products": bool(c.Item.AllProducts),
			"products": products, "amount_in_period": c.Amount,
		})
	}
	return map[string]any{
		"contract_id":              r.p.Contract.ID,
		"period_start":             string(r.p.PeriodStart),
		"period_end":               string(r.p.PeriodEnd),
		"costs":                    costs,
		"gross":                    r.p.Gross,
		"fees":                     r.p.Fees,
		"projected_costs":          r.p.ProjectedCosts,
		"estimated_net":            r.p.EstimatedNet,
		"estimated_amount":         r.p.EstimatedAmount,
		"estimated_cost_ratio_ppm": r.p.EstimatedCostRatioPPM,
	}
}
func (r projectionResource) With() map[string]any { return nil }

type costsResource []CostView

func (r costsResource) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(r))
	for _, c := range r {
		products := c.Products
		if products == nil {
			products = []string{}
		}
		items = append(items, map[string]any{
			"id": c.Item.ID, "name": c.Item.Name, "category": c.Item.Category,
			"amount": c.Item.Amount, "recurrence": string(c.Item.Recurrence),
			"starts_on": string(c.Item.StartsOn), "ends_on": string(c.Item.EndsOn),
			"all_products": bool(c.Item.AllProducts), "products": products,
		})
	}
	return map[string]any{"items": items}
}
func (r costsResource) With() map[string]any { return nil }

func invoiceFields(i *Invoice) map[string]any {
	return map[string]any{
		"id":             i.ID,
		"investor_id":    i.InvestorID,
		"contract_id":    i.ContractID,
		"period_start":   string(i.PeriodStart),
		"period_end":     string(i.PeriodEnd),
		"gross":          i.Gross,
		"fees":           i.Fees,
		"costs":          i.Costs,
		"net":            i.Net,
		"cost_ratio_ppm": i.CostRatioPPM,
		"share_bps":      i.ShareBps,
		"amount":         i.Amount,
		"status":         string(i.Status),
		"closed_at":      i.ClosedAt.UTC().Format(time.RFC3339),
	}
}

type invoicesResource []*Invoice

func (r invoicesResource) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(r))
	for _, i := range r {
		if i != nil {
			items = append(items, invoiceFields(i))
		}
	}
	return map[string]any{"items": items}
}
func (r invoicesResource) With() map[string]any { return nil }

type closingsResource struct {
	closed []*Invoice
	failed map[string]error
}

func (r closingsResource) ToArray() map[string]any {
	closed := invoicesResource(r.closed).ToArray()["items"]
	// The reason a contract was not closed is logged by the caller, not sent:
	// the admin sees which contracts to look at.
	failed := make([]string, 0, len(r.failed))
	for id := range r.failed {
		failed = append(failed, id)
	}
	return map[string]any{"closed": closed, "failed_contract_ids": failed}
}
func (r closingsResource) With() map[string]any { return nil }
