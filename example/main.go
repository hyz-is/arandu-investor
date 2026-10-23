//go:build example

// Command example wires this package the way an application does, and then
// runs one investor through a whole period: the contract, the money in, the
// costs, the closing, the invoice and its withdrawal.
//
// Run it:
//
//	go run -tags example ./example
//
// It is behind a build tag so that installing this package never compiles it.
// It uses SQLite in a temporary directory and leaves nothing behind, so it needs
// no configuration and no server. It does not serve the screens: those are
// published into a project and compiled there, and the README carries that.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	hdatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/filesystem"
	br "github.com/hyz-is/arandu-br"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

// tenant is the customer this example writes as. An application reads its own
// from configuration, and never from a request.
const tenant = "acme"

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "arandu-investor-example")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "investor.db")
	pool, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+"?_txlock=immediate")
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()

	// ---- the wiring, which is what an application copies -------------------
	//
	// arandu-wallet first: the invoices are deposited there and withdrawn from
	// there, and this package keeps no balance of its own. Each Disk is the
	// application's own -- local here, S3 in production.
	key := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(key, time.Hour, false, security.NewMemoryBackend())
	handle := data.Wrap(pool, data.DialectSQLite)
	disk := func(name string) (*filesystem.Disk, error) {
		adapter, err := filesystem.NewLocalFilesystemAdapter(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		return filesystem.NewDisk(name, adapter), nil
	}
	receipts, err := disk("receipts")
	if err != nil {
		return err
	}
	contracts, err := disk("contracts")
	if err != nil {
		return err
	}
	walletModule, err := wallet.New(wallet.Config{Tenant: tenant, Receipts: receipts}, handle, sessions)
	if err != nil {
		return err
	}
	module, err := investor.New(investor.Config{
		Files:  contracts,
		Wallet: walletModule.Service(),
		Payer:  investor.Payer{Name: "Empresa Ltda", Document: "11.222.333/0001-81"},
		// A Brazilian product: every screen, message and report in pt-BR,
		// and the document title followed by the application's APP_NAME.
		Locale:         "pt-BR",
		TitleSeparator: " — ",
	}, handle, sessions)
	if err != nil {
		return err
	}

	// In an application this is `aru migrate`, run as a pipeline step. Never at
	// boot: with N replicas, N migrations race.
	connection := hdatabase.NewConnection(pool, path, "", map[string]any{"driver": string(hdatabase.DialectSQLite), "name": "example"})
	for _, migration := range append(walletModule.Migrations(), module.Migrations()...) {
		if err := migration.Up(ctx, hdatabase.ForMigrations(connection)); err != nil {
			return fmt.Errorf("applying %s: %w", migration.GetName(), err)
		}
	}

	// Who is acting. In an application this comes off the session, with the
	// roles arandu-permission filled: the finance team is this package's admin
	// and arandu-wallet's operator; the investor needs no role.
	finance := security.Subject{ID: "finance-1", Tenant: tenant, Verified: true, Roles: []string{investor.AdminRole, wallet.OperatorRole}}
	ana := security.Subject{ID: "user-ana", Tenant: tenant, Verified: true}
	svc := module.Service()

	say("register", "an investor is a person (CPF) or a company (CNPJ), tied to a login user")
	who := must(svc.Create(ctx, finance, investor.CreateRequest{
		UserID: ana.ID, Kind: investor.Person, Document: "529.982.247-25", Name: "Ana Souza", Email: "ana@example.com",
	}))
	fmt.Printf("  %s, CPF %s\n", who.Name, br.CPF(who.Document))

	say("contract", "15% of the net of every product, closing on the 20th, with the signed PDF")
	contract := must(svc.CreateContract(ctx, finance, who.ID, investor.ContractRequest{
		Number: "C-001", SignedOn: "2026-09-15", StartsOn: "2026-09-20", ShareBps: 1500, BillingDay: 20,
		AllProducts: true, FileName: "contrato.pdf", FileType: "application/pdf",
	}, strings.NewReader("%PDF-1.4 assinado")))
	fmt.Printf("  %s v%d\n", contract.Number, contract.Version)

	say("record", "the application reports its money: a sale with the gateway's fee, and a renewal")
	for _, e := range []investor.EntryRequest{
		{ExternalRef: "mp:1", Kind: investor.KindSale, ProductID: "placaflow", OccurredAt: at(2026, 10, 5), Gross: 3000000, Fee: 30000, FeeDetail: "gateway"},
		{ExternalRef: "mp:2", Kind: investor.KindRenewal, ProductID: "placaflow", OccurredAt: at(2026, 10, 12), Gross: 1000000, Fee: 10000, FeeDetail: "gateway"},
	} {
		entry := must(svc.RecordEntry(ctx, finance, e))
		fmt.Printf("  %-8s gross %s, fee %s, net %s\n", entry.Kind, br.BRL(entry.Gross), br.BRL(entry.Fee), br.BRL(entry.Net))
	}

	say("cost", "the servers cost R$ 6.000,00 a month, shared out over the revenue")
	must(svc.CreateCost(ctx, finance, investor.CostRequest{
		Name: "Servidores GPU", Category: "infra", Amount: 600000, Recurrence: investor.Monthly, StartsOn: "2026-01-01", AllProducts: true,
	}))

	say("close", "on the 20th the period closes: the invoice is the share of the net, in the investor's wallet")
	invoice := must(svc.Close(ctx, finance, contract.ID, "2026-10-20"))
	fmt.Printf("  %s to %s: gross %s, fees %s, costs %s, net %s, 15%% = %s (%s)\n",
		invoice.PeriodStart, invoice.PeriodEnd, br.BRL(invoice.Gross), br.BRL(invoice.Fees), br.BRL(invoice.Costs),
		br.BRL(invoice.Net), br.BRL(invoice.Amount), invoice.Status)

	say("withdraw", "the investor declares a PIX key and asks for the whole invoice; the operator pays it")
	account := must(svc.AddPixAccount(ctx, ana, "email", "ana@example.com"))
	view := must(svc.WithdrawInvoice(ctx, ana, invoice.ID, account.ID))
	must(svc.StartPaying(ctx, finance, view.Withdrawal.ID))
	paid := must(svc.PayInvestorWithdrawal(ctx, finance, wallet.PaymentInput{
		RequestID: view.Withdrawal.ID, EndToEndID: "E00000000202610201200", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF-1.4 comprovante")))
	fmt.Printf("  %s by PIX to %s\n", paid.Status, paid.AccountSnapshot["label"])

	views := must(svc.InvoiceViews(ctx, ana, who.ID))
	fmt.Printf("  the invoice reads %s\n", views[0].Invoice.Status)
	return nil
}

func at(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

func say(step, what string) { fmt.Printf("\n%s: %s\n", step, what) }

// must stops the example at the first refusal, naming it.
func must[T any](value T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
	return value
}
