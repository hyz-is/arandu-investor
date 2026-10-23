package feature_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/translation"
	hview "github.com/arandu-io/hesape/view"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

// englishWords are words no Portuguese sentence carries, and every English
// validation message does.
var englishWords = regexp.MustCompile(`(?i)\b(is|are|must|required|not|be|the|cannot|choose|could|has|an|than|and|or|of|to|valid)\b`)

// rejectedMessages posts a registration with nothing filled in and answers
// the messages the redirect carries in the flash.
func rejectedMessages(t *testing.T, cfg investor.Config) validation.Errors {
	t.Helper()
	m := mount(t, cfg)
	cookies := m.signIn(t, administrator())
	rec := m.answer(t, http.MethodPost, investor.DefaultPrefix+"/admin/investors", "person_kind=person&document=1&name=&email=ana", cookies...)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an empty registration answered %d, want %d back", rec.Code, http.StatusSeeOther)
	}
	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.Header.Set("Accept", "text/html")
	for _, cookie := range rec.Result().Cookies() {
		next.AddCookie(cookie)
	}
	errs, _, ok := m.flash.Take(httptest.NewRecorder(), next)
	if !ok || len(errs) == 0 {
		t.Fatal("the redirect carries no message in the flash")
	}
	return errs
}

func TestARejectedFormComesBackInTheConfiguredLocale(t *testing.T) {
	t.Parallel()

	errs := rejectedMessages(t, investor.Config{Locale: "pt-BR"})
	want := map[string]string{
		"user_id":  "Preencha este campo.",
		"name":     "Preencha este campo.",
		"email":    "Informe um e-mail válido.",
		"document": "Informe um CPF válido.",
	}
	for field, message := range want {
		if len(errs[field]) == 0 || errs[field][0] != message {
			t.Errorf("%s came back as %v, want %q", field, errs[field], message)
		}
	}
	for field, messages := range errs {
		for _, msg := range messages {
			if word := englishWords.FindString(msg); word != "" {
				t.Errorf("%s came back in English (%q): %q", field, word, msg)
			}
		}
	}
}

func TestARejectedFormFollowsTheLocaleTheRequestNegotiated(t *testing.T) {
	t.Parallel()

	errs := rejectedMessages(t, investor.Config{Middleware: []func(http.Handler) http.Handler{translation.InLocale("pt-BR")}})
	if got := errs["name"]; len(got) == 0 || got[0] != "Preencha este campo." {
		t.Errorf("under InLocale(pt-BR) the name came back as %v", got)
	}
}

func TestEnglishStaysTheDefault(t *testing.T) {
	t.Parallel()

	errs := rejectedMessages(t, investor.Config{})
	if got := errs["name"]; len(got) == 0 || got[0] != "is required" {
		t.Errorf("with no locale the name came back as %v, want the framework's own \"is required\"", got)
	}
	if got := errs["document"]; len(got) == 0 || got[0] != "is not a valid CPF" {
		t.Errorf("with no locale the document came back as %v", got)
	}
}

// TestTheWalletsMessagesReadInPortuguese holds the messages this package does
// not write but its screens show: arandu-wallet's, when an operator pays or
// rejects a withdrawal with something missing.
func TestTheWalletsMessagesReadInPortuguese(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	account, err := w.svc.AddPixAccount(ctx, user("user-ana"), "cpf", cpfAna)
	if err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}
	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	labels := w.module.Labels("pt-BR")
	var rejected validation.Errors
	if _, err := w.svc.RejectInvestorWithdrawal(ctx, admin(), view.Withdrawal.ID, ""); !errors.As(err, &rejected) {
		t.Fatalf("rejecting with no reason answered %v, want validation.Errors", err)
	}
	var unpaid validation.Errors
	if _, err := w.svc.PayInvestorWithdrawal(ctx, admin(), wallet.PaymentInput{RequestID: view.Withdrawal.ID}, strings.NewReader("")); !errors.As(err, &unpaid) {
		t.Fatalf("paying with nothing answered %v, want validation.Errors", err)
	}
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, ""); err == nil {
		t.Fatal("asking with no account was accepted")
	}
	for name, errs := range map[string]validation.Errors{"rejecting": rejected, "paying": unpaid} {
		for field, messages := range labels.Errors(errs) {
			for _, msg := range messages {
				if word := englishWords.FindString(msg); word != "" {
					t.Errorf("%s: %s reads %q in pt-BR (%q)", name, field, msg, word)
				}
			}
		}
		// The banner names the field by its label -- "ID end-to-end" is what a
		// Brazilian bank calls it -- and the message after it is Portuguese.
		for _, line := range labels.Summary(errs) {
			label, msg, ok := strings.Cut(line, ": ")
			if !ok || label == "" {
				t.Errorf("%s: the banner line %q does not name its field", name, line)
				continue
			}
			if word := englishWords.FindString(msg); word != "" {
				t.Errorf("%s: the banner reads %q in pt-BR (%q)", name, line, word)
			}
		}
	}
}

// TestTheDocumentTitleCarriesTheApplicationsName serves a screen through the
// router with the application's name on the request, as the Application puts
// APP_NAME there, and reads the page Config.Chrome is handed: the document
// title carries the name after the separator, and the title Chrome draws as a
// heading does not.
func TestTheDocumentTitleCarriesTheApplicationsName(t *testing.T) {
	t.Parallel()
	w := install(t)

	for _, tc := range []struct {
		name      string
		separator string
		appName   string
		want      string
	}{
		{"separated", " — ", "Alpr.sh", "Saques dos investidores — Alpr.sh"},
		{"no separator", "", "Alpr.sh", "Saques dos investidores"},
		{"no name", " — ", "", "Saques dos investidores"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen investor.ChromeRequest
			stop := errors.New("drawn")
			sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
			sessions.ResolveSubjects(resolveSubjects)
			module, err := investor.New(investor.Config{
				Files: testDisk(t), Wallet: w.wallets, Locale: "pt-BR", TitleSeparator: tc.separator,
				Middleware: []func(http.Handler) http.Handler{appNamed(tc.appName)},
				Chrome: func(_ *fhttp.Context, req investor.ChromeRequest) (any, error) {
					seen = req
					return nil, stop
				},
			}, w.db, sessions)
			if err != nil {
				t.Fatalf("building the module: %v", err)
			}
			router := fhttp.NewRouter().WithFlash(security.NewFlash([]byte(appKey), false))
			module.Routes(router.ForModule(module.Name()))
			m := mounted{router: router, sessions: sessions}
			req := httptest.NewRequest(http.MethodGet, investor.DefaultPrefix+"/admin/withdrawals", nil)
			for _, cookie := range m.signIn(t, admin()) {
				req.AddCookie(cookie)
			}
			// Chrome stops the screen before a view is drawn -- this binary
			// links none -- and a bare router answers the error by panicking,
			// where an application's error page would answer it.
			func() {
				defer func() { _ = recover() }()
				router.ServeHTTP(httptest.NewRecorder(), req)
			}()

			if got := seen.Page.PageTitle(); got != tc.want {
				t.Errorf("the document title is %q, want %q", got, tc.want)
			}
			if seen.Title != "Saques dos investidores" {
				t.Errorf("Chrome is handed the title %q, want the screen's own", seen.Title)
			}
		})
	}
}

// appNamed puts an application name on the request, as the Application does
// with APP_NAME.
func appNamed(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if name != "" {
				r = r.WithContext(hhttp.WithAppName(r.Context(), name))
			}
			next.ServeHTTP(rw, r)
		})
	}
}

// drawn are the pages the stub dashboard below was handed, by investor.
var drawn sync.Map

// The dashboard is registered as a view that keeps what it is handed, so a
// test reads the page data the handler built without a compiled view in
// this binary.
func init() {
	hview.Register(investor.ViewDashboard, func(_ io.Writer, data any) error {
		page := data.(investor.DashboardPageData)
		drawn.Store(page.Investor, page)
		return nil
	})
}

// TestThePanelOffersReportsOnlyFromTheFirstContract holds what the investor's
// panel offers: no monthly report of a month before their first contract
// started, no income report of a year before it, and a contract with no end
// read as open-ended rather than as a dash.
func TestThePanelOffersReportsOnlyFromTheFirstContract(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	contractOver(t, w, ana, 1500) // starts on 2026-09-20, with no end

	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	sessions.ResolveSubjects(resolveSubjects)
	module, err := investor.New(investor.Config{Files: testDisk(t), Wallet: w.wallets, Locale: "pt-BR"}, w.db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	router := fhttp.NewRouter().WithFlash(security.NewFlash([]byte(appKey), false)).WithRenderer(hview.NewRenderer())
	module.Routes(router.ForModule(module.Name()))
	m := mounted{router: router, sessions: sessions}
	if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+"/me", "", m.signIn(t, user("user-ana"))...); rec.Code != http.StatusOK {
		t.Fatalf("the panel answered %d: %s", rec.Code, rec.Body)
	}
	value, ok := drawn.Load(ana.Name)
	if !ok {
		t.Fatal("the panel drew no dashboard")
	}
	page := value.(investor.DashboardPageData)

	now := time.Now().In(time.FixedZone("BRT", -3*60*60))
	if len(page.ReportMonths) == 0 || page.ReportMonths[0].Label != now.Format("01/2006") {
		t.Fatalf("the monthly reports start at %v, want this month first", page.ReportMonths)
	}
	for _, month := range page.ReportMonths {
		at, err := time.Parse("01/2006", month.Label)
		if err != nil || at.Before(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("a monthly report is offered for %s, before the contract started in 09/2026", month.Label)
		}
	}
	if last := page.ReportMonths[len(page.ReportMonths)-1]; len(page.ReportMonths) < 12 && last.Label != "09/2026" {
		t.Errorf("the oldest monthly report is %s, want 09/2026, the month the contract started", last.Label)
	}
	if got := page.ReportYears[len(page.ReportYears)-1].Label; got != "2026" {
		t.Errorf("the oldest income report is %s, want 2026, the year the contract started", got)
	}
	if len(page.Contracts) != 1 || page.Contracts[0].Period != "20/09/2026 — em aberto" || page.Contracts[0].Ends != "em aberto" {
		t.Errorf("the contract with no end reads %+v", page.Contracts)
	}
}
