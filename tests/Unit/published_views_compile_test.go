package unit_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/framework/foundation"
)

// viewCompiler is the command line whose view compiler the published views are
// held against, at the release an application builds them with.
//
// It is a version and never "latest", so this test changes its answer when this
// line changes and never because somebody published something. Moving it is
// how a stricter compiler is taken on: raise it, run the suite, and fix the
// view it refuses here rather than in every application that publishes it.
const viewCompiler = "github.com/arandu-io/aru@v0.72.2"

// TestEveryPublishedViewCompiles publishes the views into a project of their
// own, builds them with the view compiler an application runs, and compiles
// what it wrote.
//
// Nothing else in this repository reads the markup. The sources open with a
// build tag, so go build, go vet and go test never look past it, and the view
// compiler refuses what the Go compiler would accept -- a value written into an
// address behind text it cannot read, a value in a position no escape covers.
// A view it refuses is a view that stops the build of every application that
// publishes it, and without this test the first place to say so is somebody
// else's terminal.
//
// The project is laid out as an application's is once it has published: each
// file where the publication writes it, and a go.mod that resolves this package
// to the checkout under test. What the view compiler writes then goes through
// the Go compiler, which is what catches a field the markup names and the page
// data does not have.
//
// It does not skip. A gate that steps aside when its input is missing is how
// the views stopped compiling with nothing here saying so.
func TestEveryPublishedViewCompiles(t *testing.T) {
	root := packageRoot(t)
	project := t.TempDir()

	publications, err := foundation.Publications(module(t))
	if err != nil {
		t.Fatalf("reading what the module publishes: %v", err)
	}
	var published int
	for _, publication := range publications {
		from := publication.From
		if from == "" {
			from = "."
		}
		err := fs.WalkDir(publication.Files, from, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative := name
			if from != "." {
				relative = strings.TrimPrefix(strings.TrimPrefix(name, from), "/")
			}
			body, err := fs.ReadFile(publication.Files, name)
			if err != nil {
				return err
			}
			target := filepath.Join(project, filepath.FromSlash(path.Join(publication.To, relative)))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			published++
			return os.WriteFile(target, body, 0o644)
		})
		if err != nil {
			t.Fatalf("publishing the %s files: %v", publication.Tag, err)
		}
	}
	if published == 0 {
		t.Fatal("the module publishes no file, so this gate would compile nothing")
	}

	// The application's go.mod names this package and points it at the
	// checkout, so the views compile against the code beside them rather than
	// against a release. The sums come from here: the application needs no
	// module this package does not already require.
	goMod := readReleaseFile(t, root, "go.mod")
	language := captureReleaseValue(t, goMod, `(?m)^go ([0-9.]+)$`, "go directive in go.mod")
	modulePath := captureReleaseValue(t, goMod, `(?m)^module (\S+)$`, "module path in go.mod")
	manifest := "module example.com/application\n\n" +
		"go " + language + "\n\n" +
		"require " + modulePath + " v0.0.0-00010101000000-000000000000\n\n" +
		"replace " + modulePath + " => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.sum"), []byte(readReleaseFile(t, root, "go.sum")), 0o644); err != nil {
		t.Fatal(err)
	}

	inProject(t, project, "the published views do not compile with "+viewCompiler+", so no application that publishes them can build",
		"go", "run", viewCompiler, "view:build")
	inProject(t, project, "the views "+viewCompiler+" compiled do not build against this package",
		"go", "build", "./...")

	// Compiling is not drawing. Each screen is rendered once, with a row of
	// every kind and every action switched on, under a layout that draws the
	// content section alone -- so a page that compiles and then fails on its
	// own data, or draws a key instead of a sentence, fails here.
	probe := filepath.Join(project, "renderprobe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(renderProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inProject(t, project, "a published view does not render the page data its handler hands it",
		"go", "run", "./renderprobe")
}

// renderProbe renders every screen with sample data and checks what it drew.
const renderProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/arandu-io/hesape/view"
	investor "github.com/hyz-is/arandu-investor"

	_ "example.com/application/storage/framework/views/modules/investor"
	_ "example.com/application/storage/framework/views/modules/investor/admin"
)

func main() {
	view.RegisterLayout("layouts.app", func(w io.Writer, _ any, sections map[string]func(io.Writer) error) error {
		return sections["content"](w)
	})
	sc := investor.Screen{Labels: investor.Labels{Locale: "pt-BR"}, Prefix: "/investidor"}
	contract := investor.ContractRow{ID: "c1", Number: "C-001", Share: "15,00%", Products: "Toda a aplicação", Status: "Ativo",
		Starts: "01/10/2026", Ends: "—", Version: 1, BillingDay: 20, Active: true,
		StatementURL: "/investidor/contracts/c1/statement", ProjectionURL: "/investidor/contracts/c1/projection",
		FileURL: "/investidor/contracts/c1/file", CloseURL: "/investidor/admin/contracts/c1/closings", AmendURL: "/investidor/admin/contracts/c1/amendments"}
	invoice := investor.InvoiceRow{ID: "i1", Period: "20/09/2026 a 19/10/2026", Gross: "R$ 1.000,00", Fees: "R$ 40,00",
		Costs: "R$ 100,00", Net: "R$ 860,00", Share: "15,00%", Amount: "R$ 129,00", Status: "Disponível", StatusKey: "available",
		Withdrawable: true, WithdrawURL: "/investidor/invoices/i1/withdrawal", Withdrawal: "Pago", WithdrawalKey: "paid",
		ReceiptURL: "/investidor/invoices/i1/receipt"}
	investorRow := investor.InvestorRow{ID: "v1", Name: "Ana Souza", Document: "529.982.247-25", Kind: "Pessoa física",
		Email: "ana@example.com", Status: "Ativo", User: "user-2", URL: "/investidor/admin/investors/v1", Active: true}
	pages := []struct {
		name string
		data any
		want []string
	}{
		{investor.ViewDashboard, investor.DashboardPageData{Screen: sc, Investor: "Ana Souza", Document: "529.982.247-25",
			Available: "R$ 129,00", Held: "R$ 0,00", ReceivedYear: "R$ 129,00", WithdrawnTotal: "R$ 0,00",
			Contracts: []investor.ContractRow{contract}, Invoices: []investor.InvoiceRow{invoice},
			Accounts: []investor.AccountOption{{ID: "acc-1", Label: "PIX ana@example.com"}}, Year: 2026, Month: 10,
			ReportsURL: "/investidor/me/reports", PixURL: "/investidor/me/payout-accounts"},
			[]string{"Disponível", "R$ 129,00", "C-001", "PIX ana@example.com", "/investidor/invoices/i1/withdrawal", "/investidor/me/reports/2026/10", "Chave PIX"}},
		{investor.ViewStatement, investor.StatementPageData{Screen: sc, Contract: contract,
			Rows: []investor.StatementRow{{ID: "e1", Day: "05/10/2026", Product: "loccrm", Kind: "Venda", Description: "Plano",
				Gross: "R$ 199,00", Fee: "R$ 7,98", Cost: "R$ 19,90", Net: "R$ 171,12", Estimate: true}},
			Gross: "R$ 199,00", Fees: "R$ 7,98", Costs: "R$ 19,90", Net: "R$ 171,12", URL: contract.StatementURL, HasEstimates: true},
			[]string{"Extrato", "R$ 171,12", "loccrm", "custo estimado"}},
		{investor.ViewProjection, investor.ProjectionPageData{Screen: sc, Contract: contract, PeriodStart: "20/10/2026", PeriodEnd: "19/11/2026",
			Costs: []investor.ProjectedCostRow{{Name: "Servidor GPU", Category: "Infra", Recurrence: "Mensal", Scope: "Toda a aplicação", Amount: "R$ 600,00"}},
			Gross: "R$ 0,00", Fees: "R$ 0,00", ProjectedCosts: "R$ 600,00", Ratio: "0,00%", EstimatedNet: "R$ 0,00", EstimatedAmount: "R$ 0,00"},
			[]string{"Projeção do período", "Servidor GPU", "20/10/2026"}},
		{investor.ViewAdminInvestors, investor.AdminInvestorsPageData{Screen: sc, Rows: []investor.InvestorRow{investorRow},
			StoreURL: "/investidor/admin/investors", NextURL: "/investidor/admin/investors?cursor=v1", Next: "v1"},
			[]string{"Ana Souza", "Novo investidor", "/investidor/admin/entries", "cursor=v1"}},
		{investor.ViewAdminInvestor, investor.AdminInvestorPageData{Screen: sc, Investor: investorRow,
			Contracts: []investor.ContractRow{contract}, Invoices: []investor.InvoiceRow{invoice},
			ContractStoreURL: "/investidor/admin/investors/v1/contracts", UpdateURL: "/investidor/admin/investors/v1",
			SuspensionURL: "/investidor/admin/investors/v1/suspension", ReportsURL: "/investidor/admin/investors/v1/reports",
			Year: 2026, Month: 10, Today: "2026-10-12"},
			[]string{"Ana Souza", "Fechar período", "Registrar aditivo", "/investidor/admin/contracts/c1/amendments", "multipart/form-data", "_method"}},
		{investor.ViewAdminCosts, investor.AdminCostsPageData{Screen: sc, StoreURL: "/investidor/admin/costs", Today: "2026-10-12",
			Rows: []investor.CostRow{{ID: "k1", Name: "Servidor GPU", Category: "Infra", Amount: "R$ 600,00", Recurrence: "Mensal",
				Starts: "01/01/2026", Ends: "—", Scope: "Toda a aplicação", EndURL: "/investidor/admin/costs/k1/end"}}},
			[]string{"Custos do sistema", "Servidor GPU", "/investidor/admin/costs/k1/end", "amount_brl"}},
		{investor.ViewAdminEntries, investor.AdminEntriesPageData{Screen: sc, URL: "/investidor/admin/entries", StoreURL: "/investidor/admin/entries",
			CloseDueURL: "/investidor/admin/closings/due", Today: "2026-10-12", CurrentPage: 1, Pages: 3, Total: 120, Kind: "sale",
			Rows: []investor.EntryRow{{ID: "e1", Day: "05/10/2026", Product: "loccrm", Kind: "Venda", Gross: "R$ 199,00", Fee: "R$ 7,98",
				Net: "R$ 191,02", Source: "Aplicação", Ref: "o-1", ReverseURL: "/investidor/admin/entries/e1/reversal", Reversible: true}}},
			[]string{"Lançamentos", "R$ 191,02", "/investidor/admin/entries/e1/reversal", "Fechar os contratos que vencem hoje", "gross_brl"}},
		{investor.ViewAdminWithdrawal, investor.AdminWithdrawalsPageData{Screen: sc, URL: "/investidor/admin/withdrawals",
			Rows: []investor.WithdrawalRow{
				{ID: "w1", Investor: "Ana Souza", Amount: "R$ 129,00", Account: "PIX ana@example.com", Status: "Pedido", StatusKey: "requested",
					RequestedAt: "12/10/2026 10:00", Open: true, ProcessURL: "/investidor/admin/withdrawals/w1/processing",
					PayURL: "/investidor/admin/withdrawals/w1/payment", RejectURL: "/investidor/admin/withdrawals/w1/rejection"},
				{ID: "w2", Investor: "Bruno", Amount: "R$ 50,00", Status: "Em pagamento", StatusKey: "processing", Processing: true,
					PayURL: "/investidor/admin/withdrawals/w2/payment", RejectURL: "/investidor/admin/withdrawals/w2/rejection"},
			}},
			[]string{"Saques dos investidores", "/investidor/admin/withdrawals/w1/processing", "/investidor/admin/withdrawals/w2/payment", "end_to_end_id"}},
	}
	failed := false
	for _, p := range pages {
		rec := httptest.NewRecorder()
		if err := view.NewRenderer().Render(context.Background(), rec, 200, p.name, p.data); err != nil {
			fmt.Printf("%s: %v\n", p.name, err)
			failed = true
			continue
		}
		body := rec.Body.String()
		for _, want := range p.want {
			if !strings.Contains(body, want) {
				fmt.Printf("%s: the page does not say %q\n", p.name, want)
				failed = true
			}
		}
		if strings.Contains(body, "investor.screen.") || strings.Contains(body, "investor.field.") || strings.Contains(body, "investor.control.") {
			fmt.Printf("%s: the page draws a catalogue key instead of its sentence\n", p.name)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
`

// inProject runs one command in the scratch project, outside any workspace,
// and fails with what it printed.
//
// -mod=mod lets the go command fill in the requirements the application's
// go.mod leaves to this package's, which is what an application's own `go mod
// tidy` would have written.
func inProject(t *testing.T, dir, failure string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s:\n$ %s %s\n%s", failure, name, strings.Join(args, " "), output)
	}
}
