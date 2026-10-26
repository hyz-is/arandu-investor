package unit_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

func facetRows(values ...string) []components.TableRow {
	rows := make([]components.TableRow, 0, len(values))
	for _, v := range values {
		row := components.TableRow{}
		if v != "" {
			row.Facets = map[string]string{"status": v}
		}
		rows = append(rows, row)
	}
	return rows
}

// A filter offers the values the rows carry, in the order they first appear,
// with the count of each; one with a single value narrows nothing and is left
// out.
func TestCompleteFiltersOfferWhatTheRowsCarry(t *testing.T) {
	t.Parallel()

	filters := investor.CompleteFilters(facetRows("Ativo", "Suspenso", "Ativo", ""), components.DataTableFilter{Key: "status", Label: "Situação"})
	if len(filters) != 1 {
		t.Fatalf("filters = %d, want 1", len(filters))
	}
	var values []string
	for _, o := range filters[0].Options {
		values = append(values, o.Value)
	}
	if !reflect.DeepEqual(values, []string{"Ativo", "Suspenso"}) {
		t.Fatalf("options = %v, want the values in order of appearance", values)
	}
	if filters[0].Counts["Ativo"] != 2 || filters[0].Counts["Suspenso"] != 1 {
		t.Fatalf("counts = %v", filters[0].Counts)
	}

	if got := investor.CompleteFilters(facetRows("Ativo", "Ativo"), components.DataTableFilter{Key: "status"}); len(got) != 0 {
		t.Fatalf("a filter with one value was kept: %v", got)
	}
}

// Named options keep their words, and only the ones some row carries.
func TestCompleteFiltersKeepNamedOptionsSomeRowCarries(t *testing.T) {
	t.Parallel()

	options := []components.SelectOption{{Value: "paid", Label: "Pago"}, {Value: "rejected", Label: "Recusado"}, {Value: "requested", Label: "Pedido"}}
	filters := investor.CompleteFilters(facetRows("paid", "requested", "paid"), components.DataTableFilter{Key: "status", Options: options})
	if len(filters) != 1 || len(filters[0].Options) != 2 || filters[0].Options[0].Label != "Pago" || filters[0].Options[1].Value != "requested" {
		t.Fatalf("options = %+v", filters)
	}
}

// A list read by cursor knows the address of the next page and nothing else.
func TestNextOnlyKnowsTheNextAddressAndNothingElse(t *testing.T) {
	t.Parallel()

	page := investor.NextOnly("/investors?cursor=v1")
	if !page.HasPages() || !page.HasMorePages() || !page.OnFirstPage() || page.PreviousPageURL() != "" || page.NextPageURL() != "/investors?cursor=v1" {
		t.Fatalf("page = %+v", page)
	}
	last := investor.NextOnly("")
	if last.HasPages() || last.HasMorePages() || last.NextPageURL() != "" {
		t.Fatalf("the last page offers a step: %+v", last)
	}
}
