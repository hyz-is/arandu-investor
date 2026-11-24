package investor

import (
	"context"

	"github.com/arandu-io/framework/security"
)

// monthOf is the calendar month of a day, "2026-11": what the tax of a month
// is computed over.
func monthOf(day Day) string {
	if len(day) < 7 {
		return ""
	}
	return string(day[:7])
}

// retention is the Withholder the service asks: the application's, or the one
// the package carries.
func (s *InvestorService) retention() Withholder {
	if s.withholder != nil {
		return s.withholder
	}
	return DefaultWithholder()
}

// withhold computes the retention of one payment credited on the day, after
// the caller authorized: it reads what the investor was already credited and
// held in the same month, from the invoices that were held over, and asks the
// Withholder.
func (s *InvestorService) withhold(ctx context.Context, g security.Grant, owner *Investor, day Day, gross int64) (Withholding, error) {
	prior, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		Where("withholding_month", "=", monthOf(day)).Get(ctx, g)
	if err != nil {
		return Withholding{}, err
	}
	var accrued WithholdingAccrued
	for _, inv := range prior {
		accrued.Gross += inv.GrossPart()
		accrued.INSSBase += inv.INSSBase
		accrued.INSS += inv.INSSAmount
		accrued.IRRF += inv.IRRFAmount
	}
	return s.retention().Withhold(ctx, WithholdingInput{Day: day, Gross: gross, Dependents: owner.Dependents, Accrued: accrued})
}

// NextPayoutOf is the next day the investor who is asking is paid, from the
// pay day their contracts in force set: the earliest of them on or after the
// day given. It answers the zero Day when no contract sets one, and the
// application's own day (Config.NextPayout) is the one to show then.
func (s *InvestorService) NextPayoutOf(ctx context.Context, actor security.Subject, from Day) (Day, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Investor{})
	if err != nil {
		return "", err
	}
	me, err := Investors(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return "", err
	}
	if me == nil {
		return "", ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
		return "", err
	}
	contracts, err := Contracts(s.db).Where("investor_id", "=", me.ID).
		Where("status", "=", string(ContractActive)).Where("pay_day", ">", 0).Get(ctx, g)
	if err != nil {
		return "", err
	}
	var next Day
	for _, c := range contracts {
		if !c.InForce(from) {
			continue
		}
		if day := NextPayDay(c.PayDay, from); next.IsZero() || day.Before(next) {
			next = day
		}
	}
	return next, nil
}
