package investor

import (
	"context"

	"github.com/arandu-io/framework/security"
	activitylog "github.com/hyz-is/arandu-activitylog"
)

// ActivityLogName is the log every entry of this package is recorded in.
const ActivityLogName = "investor"

// The events this package records, one per act that changes something. The
// subject of each entry is the record the act was on, by kind and key.
const (
	EventInvestorCreated     = "investor.created"
	EventInvestorUpdated     = "investor.updated"
	EventInvestorSuspended   = "investor.suspended"
	EventInvestorReactivated = "investor.reactivated"
	EventContractCreated     = "contract.created"
	EventContractAmended     = "contract.amended"
	EventEntryRecorded       = "entry.recorded"
	EventEntryReversed       = "entry.reversed"
	EventCostCreated         = "cost.created"
	EventCostEnded           = "cost.ended"
	EventPeriodClosed        = "period.closed"
	EventPayoutAccountAdded  = "payout_account.added"
	EventWithdrawalRequested = "withdrawal.requested"
	EventWithdrawalTaken     = "withdrawal.processing"
	EventWithdrawalPaid      = "withdrawal.paid"
	EventWithdrawalRejected  = "withdrawal.rejected"
)

// record writes one entry of the act just done, under the Grant it was
// authorized with: the tenant of the entry is the act's, data.Tenant(g), and
// the causer is the subject who acted. Without Config.Activity it records
// nothing.
//
// The act already happened when this runs, so a failure to record it is
// returned to the caller rather than swallowed: an act the log does not show
// is the one an audit asks about.
func (s *InvestorService) record(ctx context.Context, g security.Grant, actor security.Subject, event, kind, id string, properties map[string]any) error {
	if s.activity == nil {
		return nil
	}
	description := event
	if s.describe != nil {
		description = s.describe(event)
	}
	entry := s.activity.In(ctx, g, ActivityLogName).Event(event).
		OnRef(activitylog.Ref{Type: kind, ID: id})
	if actor.ID != "" {
		entry = entry.ByRef(activitylog.Ref{Type: "user", ID: actor.ID})
	}
	if len(properties) > 0 {
		entry = entry.WithProperties(properties)
	}
	_, err := entry.Log(description)
	return err
}
