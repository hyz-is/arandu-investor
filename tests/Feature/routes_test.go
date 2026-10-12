package feature_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/filesystem"

	investor "github.com/hyz-is/arandu-investor"
	wallet "github.com/hyz-is/arandu-wallet"
)

// These tests drive the module the way an application does: build it, register
// its routes on a router, and make a request.
//
// The database handle wraps nothing, and that is the assertion. A request that
// reached a statement would panic, so every answer below is proof that the
// refusal happened in the guard or the policy and not after a read.

// appKey is the key a session store is built over. Any thirty-two bytes will
// do here; a real application reads its own from the environment.
const appKey = "0123456789abcdef0123456789abcdef"

// reservedPrefix is the namespace the framework keeps for itself: the health
// probe, the reload endpoint, the development console, the addressed assets.
//
// A module that registers under it is refused when the application boots, by
// name -- and that refusal happens in the process of whoever installed this
// package, after it was published. Here the same rule is a failing test, in the
// repository that can still fix it.
const reservedPrefix = "/_arandu"

// mounted is the module registered on a router the way the kernel builds one,
// with the session store its guard reads and the flash its router answers a
// rejected input with.
type mounted struct {
	router   *fhttp.Router
	sessions *security.SessionStore
	flash    *security.Flash
}

// mount builds the module and returns a router with its routes registered.
func mount(t *testing.T, cfg investor.Config) mounted {
	t.Helper()

	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	flash := security.NewFlash([]byte(appKey), false)

	if cfg.Files == nil {
		cfg.Files = testDisk(t)
	}
	handle := data.Wrap(nil, data.DialectSQLite)
	if cfg.Wallet == nil {
		cfg.Wallet = testWallet(t, handle)
	}
	module, err := investor.New(cfg, handle, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}

	// WithFlash is what the kernel does at boot. Without it the router has
	// nowhere to put a rejected input and answers it as a failure.
	router := fhttp.NewRouter().WithFlash(flash)
	module.Routes(router.ForModule(module.Name()))
	return mounted{router: router, sessions: sessions, flash: flash}
}

// administrator is the most privileged subject an application can produce.
func administrator() security.Subject {
	return security.Subject{ID: "user-1", Tenant: "acme", Roles: []string{"admin"}, Verified: true}
}

// signIn starts a session for the subject and returns the cookies a browser
// would send back with the next request.
func (m mounted) signIn(t *testing.T, subject security.Subject) []*http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if _, err := m.sessions.Start(context.Background(), rec, subject); err != nil {
		t.Fatalf("starting a session: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("starting a session wrote no cookie")
	}
	return cookies
}

// answer makes one request against the router and returns the recorder.
//
// A panic out of the router is a failure of this test rather than of the run:
// it is how an error nobody claimed arrives here, and reporting it by name says
// which request produced it.
func (m mounted) answer(t *testing.T, method, target, body string, cookies ...*http.Cookie) (rec *httptest.ResponseRecorder) {
	t.Helper()

	contentType := "application/x-www-form-urlencoded"
	switch body {
	case multipartContract:
		body, contentType = contractUpload()
	case multipartReceipt:
		body, contentType = receiptUpload()
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec = httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s %s panicked, which a deployed router answers with 500: %v", method, target, recovered)
		}
	}()
	m.router.ServeHTTP(rec, req)
	return rec
}

// everyRoute is a request to each route the module registers, under the
// default prefix. TestTheModuleRegistersItsRoutesUnderItsPrefix fails when a
// route exists that this list does not reach.
//
// ownRow marks the routes whose policy answers the probe for any signed-in
// subject and decides on the row instead: the investor's own panel reads the
// row of the user who is asking, so there is no refusal to give before reading.
var everyRoute = []struct {
	method string
	target string
	body   string
	ownRow bool
}{
	{http.MethodGet, investor.DefaultPrefix + "/me", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors", "", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/investors", validPersonForm, false},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors/record-1", "", false},
	{http.MethodPut, investor.DefaultPrefix + "/admin/investors/record-1", "name=Ana&email=ana%40example.com", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/investors/record-1/suspension", "", false},
	{http.MethodDelete, investor.DefaultPrefix + "/admin/investors/record-1/suspension", "", false},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors/record-1/contracts", "", true},
	{http.MethodPost, investor.DefaultPrefix + "/admin/investors/record-1/contracts", multipartContract, false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/contracts/record-1/amendments", multipartContract, false},
	{http.MethodGet, investor.DefaultPrefix + "/me/contracts", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/contracts/record-1/file", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/contracts/record-1/statement", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/contracts/record-1/projection", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/costs", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/me/invoices", "", true},
	{http.MethodPost, investor.DefaultPrefix + "/invoices/record-1/withdrawal", "account_id=acc-1", true},
	{http.MethodGet, investor.DefaultPrefix + "/me/reports/2026/10", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/me/reports/2026", "", true},
	{http.MethodPost, investor.DefaultPrefix + "/admin/entries", "external_ref=o-1&kind=sale&product_id=loccrm&occurred_at=2026-10-05T12%3A00%3A00-03%3A00&gross=19900&fee=798", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/entries/record-1/reversal", "", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/costs", "name=Servidor&category=infra&amount=60000&recurrence=monthly&starts_on=2026-01-01&all_products=1", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/costs/record-1/end", "ends_on=2026-12-31", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/contracts/record-1/closings", "as_of=2026-10-20", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/closings/due", "as_of=2026-10-20", false},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors/record-1/invoices", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors/record-1/reports/2026/10", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/admin/investors/record-1/reports/2026", "", true},
	{http.MethodGet, investor.DefaultPrefix + "/admin/entries", "", false},
	{http.MethodGet, investor.DefaultPrefix + "/admin/costs", "", false},
	{http.MethodGet, investor.DefaultPrefix + "/admin/withdrawals", "", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/withdrawals/record-1/processing", "", false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/withdrawals/record-1/payment", multipartReceipt, false},
	{http.MethodPost, investor.DefaultPrefix + "/admin/withdrawals/record-1/rejection", "reason=dados+divergentes", false},
	{http.MethodGet, investor.DefaultPrefix + "/invoices/record-1/receipt", "", true},
	{http.MethodPost, investor.DefaultPrefix + "/me/payout-accounts", "pix_key_type=email&pix_key=ana%40example.com", true},
}

// validPersonForm is a registration every check passes, so a refusal of it is
// the policy's and not the validation's.
const validPersonForm = "user_id=user-2&person_kind=person&document=529.982.247-25&name=Ana&email=ana%40example.com"

func TestAVisitorWithNoSessionIsSentToSignIn(t *testing.T) {
	t.Parallel()

	m := mount(t, investor.Config{})

	// RequireAuth answers before the handler runs, so a visitor with no
	// session reaches neither the policy nor the database: the answer is the
	// sign-in screen, not a refusal from a policy that was asked about nobody.
	for _, request := range everyRoute {
		rec := m.answer(t, request.method, request.target, request.body)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != middleware.SignInPath {
			t.Errorf("%s %s answered %d to %q, want %d to %s",
				request.method, request.target, rec.Code, rec.Header().Get("Location"),
				http.StatusSeeOther, middleware.SignInPath)
		}
	}
}

func TestASignedInSubjectReachesThePolicyAndIsRefused(t *testing.T) {
	t.Parallel()

	m := mount(t, investor.Config{})
	cookies := m.signIn(t, administrator())

	// The guard admits the session and puts its subject on the request; the
	// handler hands it to the service, and the closed policy refuses it. The
	// router turns that refusal into 403 -- before any statement, which the
	// handle over no database would have panicked on.
	for _, request := range everyRoute {
		if request.ownRow {
			continue
		}
		rec := m.answer(t, request.method, request.target, request.body, cookies...)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d for a signed-in administrator, want %d",
				request.method, request.target, rec.Code, http.StatusForbidden)
		}
	}
}

func TestARejectedInputGoesBackBeforeTheDatabase(t *testing.T) {
	t.Parallel()

	m := mount(t, investor.Config{})
	cookies := m.signIn(t, administrator())

	// The input is validated before anything is authorized, so this refusal
	// comes from the request and not from the policy. The router answers it the
	// way it answers every rejected form: back where it came from, with the
	// messages in the flash -- and it still never reaches a statement.
	rec := m.answer(t, http.MethodPost, investor.DefaultPrefix+"/admin/investors", "person_kind=person&document=1&name=", cookies...)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("an empty name answered %d to %q, want %d back to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}

	// The page the redirect lands on is a navigation, which is the only read the
	// flash spends itself on.
	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.Header.Set("Accept", "text/html")
	for _, cookie := range rec.Result().Cookies() {
		next.AddCookie(cookie)
	}
	errs, _, ok := m.flash.Take(httptest.NewRecorder(), next)
	if !ok || len(errs["name"]) == 0 {
		t.Fatalf("the redirect carries no message for name in the flash: %v", errs)
	}
}

// TestTheRouterAnswersAMissingRecordWith404 holds the half of ErrNotFound the
// unit suite cannot: that the router this module registers on answers it, with
// no mapping written in the module.
func TestTheRouterAnswersAMissingRecordWith404(t *testing.T) {
	t.Parallel()

	router := fhttp.NewRouter()
	router.Action(http.MethodGet, "/missing", func(*fhttp.Context) error {
		return investor.ErrNotFound
	})
	m := mounted{router: router}

	if rec := m.answer(t, http.MethodGet, "/missing", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("ErrNotFound answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTheModuleRegistersItsRoutesUnderItsPrefix(t *testing.T) {
	t.Parallel()

	m := mount(t, investor.Config{Prefix: "/widgets"})

	got := make([]string, 0, len(everyRoute))
	for _, route := range m.router.Routes() {
		if route.Module != "investor" {
			t.Errorf("the route %s %s is not tagged with the module name: %q", route.Method, route.Pattern, route.Module)
		}
		got = append(got, route.Method+" "+route.Pattern)
	}
	sort.Strings(got)

	want := []string{
		"DELETE /widgets/admin/investors/{id}/suspension",
		"GET /widgets/admin/costs",
		"GET /widgets/admin/entries",
		"GET /widgets/admin/investors",
		"GET /widgets/admin/investors/{id}",
		"GET /widgets/admin/investors/{id}/contracts",
		"GET /widgets/admin/investors/{id}/invoices",
		"GET /widgets/admin/investors/{id}/reports/{year}",
		"GET /widgets/admin/investors/{id}/reports/{year}/{month}",
		"GET /widgets/admin/withdrawals",
		"GET /widgets/contracts/{id}/file",
		"GET /widgets/contracts/{id}/projection",
		"GET /widgets/contracts/{id}/statement",
		"GET /widgets/costs",
		"GET /widgets/invoices/{id}/receipt",
		"GET /widgets/me",
		"GET /widgets/me/contracts",
		"GET /widgets/me/invoices",
		"GET /widgets/me/reports/{year}",
		"GET /widgets/me/reports/{year}/{month}",
		"POST /widgets/admin/closings/due",
		"POST /widgets/admin/contracts/{id}/amendments",
		"POST /widgets/admin/contracts/{id}/closings",
		"POST /widgets/admin/costs",
		"POST /widgets/admin/costs/{id}/end",
		"POST /widgets/admin/entries",
		"POST /widgets/admin/entries/{id}/reversal",
		"POST /widgets/admin/investors",
		"POST /widgets/admin/investors/{id}/contracts",
		"POST /widgets/admin/investors/{id}/suspension",
		"POST /widgets/admin/withdrawals/{id}/payment",
		"POST /widgets/admin/withdrawals/{id}/processing",
		"POST /widgets/admin/withdrawals/{id}/rejection",
		"POST /widgets/invoices/{id}/withdrawal",
		"POST /widgets/me/payout-accounts",
		"PUT /widgets/admin/investors/{id}",
	}

	if len(got) != len(want) || len(got) != len(everyRoute) {
		t.Fatalf("registered %v, want %v, and everyRoute reaches %d of them", got, want, len(everyRoute))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registered %v, want %v", got, want)
		}
	}
}

// TestNoRouteLandsInTheFrameworkNamespace is the one property of this package
// that syntax cannot hold, and it is why it is checked here rather than beside
// the other four in tests/Unit/audit_test.go: a prefix arrives through
// configuration, so the only way to know where the routes ended up is to
// register them and read the table back.
//
// Both the default and a configured prefix are mounted, because the two reach
// the router by different paths and only one of them is written in this
// repository.
func TestNoRouteLandsInTheFrameworkNamespace(t *testing.T) {
	t.Parallel()

	for _, cfg := range []investor.Config{
		{},
		{Prefix: "/widgets"},
	} {
		m := mount(t, cfg)

		registered := 0
		for _, route := range m.router.Routes() {
			registered++
			if route.Pattern == reservedPrefix || strings.HasPrefix(route.Pattern, reservedPrefix+"/") {
				t.Errorf("the route %s %s is registered under %s/, which the framework keeps for itself and refuses at boot",
					route.Method, route.Pattern, reservedPrefix)
			}
		}
		if registered == 0 {
			t.Fatal("the module registered no route, so this test proved nothing")
		}
	}
}

func TestNewRefusesAWiringThatCannotWork(t *testing.T) {
	t.Parallel()

	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	handle := data.Wrap(nil, data.DialectSQLite)
	valid := investor.Config{Files: testDisk(t), Wallet: testWallet(t, handle)}

	if _, err := investor.New(investor.Config{Wallet: valid.Wallet}, handle, sessions); err == nil {
		t.Error("a configuration with no Disk for the contracts was accepted")
	}
	if _, err := investor.New(investor.Config{Files: valid.Files}, handle, sessions); err == nil {
		t.Error("a configuration with no arandu-wallet was accepted")
	}
	if _, err := investor.New(investor.Config{PageSize: -1, Files: valid.Files, Wallet: valid.Wallet}, handle, sessions); err == nil {
		t.Error("a configuration with a negative page size was accepted")
	}
	if _, err := investor.New(valid, nil, sessions); err == nil {
		t.Error("a nil database handle was accepted")
	}
	if _, err := investor.New(valid, handle, nil); err == nil {
		t.Error("a nil session store was accepted")
	}
	if _, err := investor.New(valid, handle, sessions); err != nil {
		t.Fatalf("a valid wiring was refused: %v", err)
	}
}

func TestNewRefusesARoutePrefixThatCannotBeRegistered(t *testing.T) {
	t.Parallel()

	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	handle := data.Wrap(nil, data.DialectSQLite)

	for _, prefix := range []string{"/widgets{", "/widgets/{id}"} {
		if _, err := investor.New(investor.Config{Prefix: prefix, Files: testDisk(t), Wallet: testWallet(t, handle)}, handle, sessions); err == nil {
			t.Errorf("New accepted route prefix %q, which would panic during route registration", prefix)
		}
	}
}

func TestTheModuleDeclaresItsSchema(t *testing.T) {
	t.Parallel()

	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	handle := data.Wrap(nil, data.DialectSQLite)
	module, err := investor.New(investor.Config{Files: testDisk(t), Wallet: testWallet(t, handle)}, handle, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}

	declared := module.Migrations()
	if len(declared) == 0 {
		t.Fatal("the module declares migrations = true and returns none")
	}

	names := make([]string, 0, len(declared))
	for _, migration := range declared {
		name := migration.GetName()
		if name == "" {
			t.Fatal("a migration has no name, and the name is what carries the order")
		}
		names = append(names, name)

		// A migration that cannot be rolled back is a deploy that cannot be
		// undone. The migrator finds Down by type assertion, so a Down with the
		// wrong signature is a rollback that silently does nothing.
		if _, ok := migration.(migrations.ReversibleMigration); !ok {
			t.Errorf("the migration %s has no Down", name)
		}
	}

	if !sort.StringsAreSorted(names) {
		t.Fatalf("the migrations are not returned in the order their names sort in: %v", names)
	}
}

// testDisk is a Disk over a fresh directory, the application's Disk in small.
func testDisk(t testing.TB) *filesystem.Disk {
	t.Helper()
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("building a local Disk: %v", err)
	}
	return filesystem.NewDisk("contracts", adapter)
}

// multipartContract stands in everyRoute for a valid contract form, which
// answer builds as multipart with a signed file.
const multipartContract = "<multipart contract>"

// contractUpload is a contract every check passes, as a browser sends it.
func contractUpload() (string, string) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for field, value := range map[string]string{
		"number": "C-001", "signed_on": "2026-10-01", "starts_on": "2026-10-01",
		"share_bps": "1500", "billing_day": "20", "all_products": "1",
	} {
		_ = w.WriteField(field, value)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="contrato.pdf"`)
	header.Set("Content-Type", "application/pdf")
	part, _ := w.CreatePart(header)
	_, _ = part.Write([]byte("%PDF-1.4 signed"))
	_ = w.Close()
	return body.String(), w.FormDataContentType()
}

// multipartReceipt stands in everyRoute for a valid payment form, which
// answer builds as multipart with a receipt.
const multipartReceipt = "<multipart receipt>"

// receiptUpload is a payment every check passes, as a browser sends it.
func receiptUpload() (string, string) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("end_to_end_id", "E00000000202610201200abcdef123456")
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="receipt"; filename="comprovante.pdf"`)
	header.Set("Content-Type", "application/pdf")
	part, _ := w.CreatePart(header)
	_, _ = part.Write([]byte("%PDF-1.4 receipt"))
	_ = w.Close()
	return body.String(), w.FormDataContentType()
}

// testWallet is arandu-wallet over the same handle, the one instance an
// application passes in Config.Wallet.
func testWallet(t testing.TB, db *data.DB) *wallet.WalletService {
	t.Helper()
	sessions := security.NewSessionStore([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false, security.NewMemoryBackend())
	module, err := wallet.New(wallet.Config{Tenant: "acme"}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	return module.Service()
}
