package feature_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"

	investor "github.com/hyz-is/arandu-investor"
)

// Two valid CPFs, so two investors can exist at once.
const (
	cpfAna   = "529.982.247-25"
	cpfBruno = "111.444.777-35"
)

func contractTerms() investor.ContractRequest {
	return investor.ContractRequest{
		Number: "C-001", SignedOn: "2026-10-01", StartsOn: "2026-10-01",
		ShareBps: 1500, BillingDay: 20, Products: []string{"loccrm"},
		FileName: "contrato.pdf", FileType: "application/pdf",
	}
}

func TestAnInvestorIsStoredDigitsOnlyAndOncePerDocument(t *testing.T) {
	t.Parallel()
	w := install(t)

	ana := registerPerson(t, w, "user-ana", cpfAna)
	if ana.Document != "52998224725" || ana.Status != investor.InvestorActive || ana.TenantID != tenant {
		t.Fatalf("stored %q status %q tenant %q, want the CPF digits only, active, in %s", ana.Document, ana.Status, ana.TenantID, tenant)
	}

	// The same document again is the same investor, and the database is what
	// refuses it: a check in Go is one two concurrent requests both pass.
	_, err := w.svc.Create(context.Background(), admin(), investor.CreateRequest{
		UserID: "user-other", Kind: investor.Person, Document: "52998224725", Name: "Outra", Email: "o@example.com",
	})
	if !errors.Is(err, database.ErrUniqueViolation) {
		t.Fatalf("a second investor with the same CPF answered %v, want a unique violation", err)
	}
}

func TestTheInvestorReadsThemselvesAndNobodyElse(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	bruno := registerPerson(t, w, "user-bruno", cpfBruno)

	me, err := w.svc.Mine(context.Background(), user("user-ana"))
	if err != nil || me.ID != ana.ID {
		t.Fatalf("Mine answered %v, %v; want Ana's row", me, err)
	}
	if _, err := w.svc.Find(context.Background(), user("user-ana"), bruno.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Ana reached Bruno's row through Find: %v", err)
	}
	if _, err := w.svc.InvestorContracts(context.Background(), user("user-ana"), bruno.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("Ana listed Bruno's contracts: %v", err)
	}
	if _, err := w.svc.Mine(context.Background(), user("user-nobody")); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("a user who is no investor answered %v, want ErrNotFound", err)
	}

	// Another tenant's admin sees none of it: the read is scoped by the Grant.
	other := security.Subject{ID: "finance-x", Tenant: "globex", Roles: []string{investor.AdminRole}, Verified: true}
	if _, err := w.svc.Find(context.Background(), other, ana.ID); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("another tenant's admin read Ana: %v", err)
	}
}

func TestAContractKeepsItsFileAndItsAmendmentsKeepTheHistory(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)

	original, err := w.svc.CreateContract(ctx, admin(), ana.ID, contractTerms(), strings.NewReader("%PDF-1.4 original"))
	if err != nil {
		t.Fatalf("registering the contract: %v", err)
	}
	if original.Version != 1 || original.RootID != original.ID || original.Status != investor.ContractActive {
		t.Fatalf("the original is version %d root %q status %q", original.Version, original.RootID, original.Status)
	}

	terms := contractTerms()
	terms.ShareBps = 2000
	terms.AllProducts = true
	amendment, err := w.svc.AmendContract(ctx, admin(), original.ID, terms, strings.NewReader("%PDF-1.4 amendment"))
	if err != nil {
		t.Fatalf("amending: %v", err)
	}
	if amendment.Version != 2 || amendment.RootID != original.ID {
		t.Fatalf("the amendment is version %d of root %q, want 2 of %q", amendment.Version, amendment.RootID, original.ID)
	}

	listed, err := w.svc.InvestorContracts(ctx, user("user-ana"), ana.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("Ana's contracts: %d, %v; want both versions", len(listed), err)
	}
	statuses := map[any]any{}
	for _, c := range listed {
		row := c.ToArray()
		statuses[row["version"]] = row["status"]
	}
	if statuses[1] != string(investor.ContractSuperseded) || statuses[2] != string(investor.ContractActive) {
		t.Fatalf("versions and statuses %v, want 1 superseded and 2 active", statuses)
	}

	// The investor downloads their own file; nobody else does.
	_, file, err := w.svc.ContractFile(ctx, user("user-ana"), original.ID)
	if err != nil {
		t.Fatalf("Ana opening her contract: %v", err)
	}
	body, _ := io.ReadAll(file.Body)
	_ = file.Body.Close()
	if string(body) != "%PDF-1.4 original" {
		t.Fatalf("the file came back as %q", body)
	}
	if _, _, err := w.svc.ContractFile(ctx, user("user-bruno"), original.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else opened Ana's contract: %v", err)
	}
}

func TestAContractNeedsItsTermsAndItsFile(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)

	refused := map[string]func(*investor.ContractRequest){
		"a share above 100%":          func(r *investor.ContractRequest) { r.ShareBps = 10001 },
		"a billing day of 31":         func(r *investor.ContractRequest) { r.BillingDay = 31 },
		"an end before the start":     func(r *investor.ContractRequest) { r.EndsOn = "2026-09-30" },
		"no product and no coverage":  func(r *investor.ContractRequest) { r.Products = nil },
		"a file that is not PDF/JPEG": func(r *investor.ContractRequest) { r.FileType = "text/html" },
	}
	for name, change := range refused {
		terms := contractTerms()
		change(&terms)
		_, err := w.svc.CreateContract(context.Background(), admin(), ana.ID, terms, strings.NewReader("%PDF"))
		var errs validation.Errors
		if !errors.As(err, &errs) {
			t.Errorf("%s answered %v, want validation errors", name, err)
		}
	}
	_, err := w.svc.CreateContract(context.Background(), admin(), ana.ID, contractTerms(), strings.NewReader(""))
	var errs validation.Errors
	if !errors.As(err, &errs) || !errs.Has("file") {
		t.Fatalf("an empty file answered %v, want a file error", err)
	}
}
