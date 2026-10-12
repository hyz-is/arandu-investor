package feature_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	hedatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"

	investor "github.com/hyz-is/arandu-investor"
	wallet "github.com/hyz-is/arandu-wallet"
)

// dsn opens a database file the way the tests need it: the write lock taken
// when a transaction begins, readers beside a writer, and a wait instead of an
// error when the lock is held.
const dsn = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)"

// tenant is the customer every test runs as, except where a second one is the
// point.
const tenant = "acme"

// world is one application with this package installed over a real database.
type world struct {
	db      *data.DB
	module  *investor.Module
	svc     *investor.InvestorService
	wallets *wallet.WalletService
}

// install opens a fresh SQLite database, applies the package's migrations and
// builds the module over it, with a Disk for the contracts.
func install(t *testing.T) world {
	t.Helper()

	path := filepath.Join(t.TempDir(), "investor.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+dsn)
	if err != nil {
		t.Fatalf("opening SQLite: %v", err)
	}
	handle.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = handle.Close() })

	connection := hedatabase.NewConnection(handle, path, "", map[string]any{
		"driver": string(hedatabase.DialectSQLite),
		"name":   "investor-test",
	})
	db := data.Wrap(handle, data.DialectSQLite)
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	walletModule, err := wallet.New(wallet.Config{Tenant: tenant}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	module, err := investor.New(investor.Config{Files: testDisk(t), Wallet: walletModule.Service()}, db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	migrations := hedatabase.ForMigrations(connection)
	for _, migration := range append(walletModule.Migrations(), module.Migrations()...) {
		if err := migration.Up(context.Background(), migrations); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return world{db: db, module: module, svc: module.Service(), wallets: walletModule.Service()}
}

// admin is the finance team of the tenant: this package's admin, and
// arandu-wallet's operator, because closing a period deposits money.
func admin() security.Subject {
	return security.Subject{ID: "finance-1", Tenant: tenant, Roles: []string{investor.AdminRole, wallet.OperatorRole}, Verified: true}
}

// user is a signed-in person of the tenant with no role of this package.
func user(id string) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true}
}

// registerPerson registers an investor and fails the test if it cannot.
func registerPerson(t *testing.T, w world, userID, cpf string) *investor.Investor {
	t.Helper()
	record, err := w.svc.Create(context.Background(), admin(), investor.CreateRequest{
		UserID: userID, Kind: investor.Person, Document: cpf, Name: "Ana " + userID, Email: userID + "@example.com",
	})
	if err != nil {
		t.Fatalf("registering %s: %v", userID, err)
	}
	return record
}
