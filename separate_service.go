package investor

import (
	"context"
	"sort"
	"strings"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/translation"
)

// The products kept apart. A contract over "the whole application" and a cost
// of "the whole application" cover every product except these; a contract or a
// cost that names one covers it as it always did. It is how an application
// keeps the line of another partner out of the contracts that cover the rest,
// without writing a list of the others' products on each of them.

// EventProductSeparated and EventProductJoined are the acts of keeping a
// product apart and of bringing it back.
const (
	EventProductSeparated = "product.separated"
	EventProductJoined    = "product.joined"
)

// SeparateProduct keeps a product apart from "the whole application". It is
// effective on the closings made after it: an invoice already closed keeps
// what it counted. Separating a product twice is the same as once.
func (s *InvestorService) SeparateProduct(ctx context.Context, actor security.Subject, productID string) error {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return validation.Errors{"product_id": {message("required")}}
	}
	if len(productID) > maxIdentifierLen {
		return validation.Errors{"product_id": {message("product_too_long", translation.Replace{"product": productID, "max": maxIdentifierLen})}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return err
	}
	found, err := SeparateProducts(s.db).Where("product_id", "=", productID).First(ctx, g)
	if err != nil {
		return err
	}
	if found != nil {
		return nil
	}
	record, err := SeparateProducts(s.db).New()
	if err != nil {
		return err
	}
	if record.ID, err = data.NewID(); err != nil {
		return err
	}
	now := s.now()
	record.TenantID = data.Tenant(g)
	record.ProductID = productID
	record.CreatedBy = actor.ID
	record.CreatedAt, record.UpdatedAt = now, now
	if _, err := record.Save(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, EventProductSeparated, "investor_product", record.ID, map[string]any{"product_id": productID})
}

// JoinProduct brings a separate product back into "the whole application".
// Bringing back one that was never kept apart changes nothing.
func (s *InvestorService) JoinProduct(ctx context.Context, actor security.Subject, productID string) error {
	productID = strings.TrimSpace(productID)
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return err
	}
	found, err := SeparateProducts(s.db).Where("product_id", "=", productID).First(ctx, g)
	if err != nil {
		return err
	}
	if found == nil {
		return nil
	}
	if _, err := found.Delete(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, EventProductJoined, "investor_product", found.ID, map[string]any{"product_id": productID})
}

// SeparateProducts lists the products kept apart, by name.
func (s *InvestorService) SeparateProducts(ctx context.Context, actor security.Subject) ([]string, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	rows, err := SeparateProducts(s.db).OrderBy("product_id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ProductID)
	}
	sort.Strings(out)
	return out, nil
}

// separateSet reads the products kept apart, after the caller authorized.
func (s *InvestorService) separateSet(ctx context.Context, g security.Grant) (map[string]bool, error) {
	rows, err := SeparateProducts(s.db).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		set[row.ProductID] = true
	}
	return set, nil
}
