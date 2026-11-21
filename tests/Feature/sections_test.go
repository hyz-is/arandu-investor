package feature_test

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	hview "github.com/arandu-io/hesape/view"

	investor "github.com/hyz-is/arandu-investor"
)

// The investor's area is a handful of pages, each with only what is its own,
// and a menu that says where the others are. These tests read the page data
// each handler builds, by registering the pages as views that keep what they
// are handed, so no compiled view has to be in this binary.

// pagesDrawn keeps the data of the pages below, by the page's view name and
// the investor's name.
var pagesDrawn sync.Map

func init() {
	hview.Register(investor.ViewWithdrawals, func(_ io.Writer, data any) error {
		page := data.(investor.WithdrawalsPageData)
		pagesDrawn.Store(investor.ViewWithdrawals+"/"+page.Investor, page)
		return nil
	})
	hview.Register(investor.ViewContract, func(_ io.Writer, data any) error {
		page := data.(investor.ContractPageData)
		pagesDrawn.Store(investor.ViewContract+"/"+page.Investor, page)
		return nil
	})
	hview.Register(investor.ViewReports, func(_ io.Writer, data any) error {
		page := data.(investor.ReportsPageData)
		pagesDrawn.Store(investor.ViewReports+"/"+page.Investor, page)
		return nil
	})
}

// areaOf mounts the module over the world's database with the configuration
// given, and returns the router the requests are made on.
func areaOf(t *testing.T, w world, cfg investor.Config) (*investor.Module, mounted) {
	t.Helper()
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	sessions.ResolveSubjects(resolveSubjects)
	cfg.Files, cfg.Wallet, cfg.Locale = testDisk(t), w.wallets, "pt-BR"
	module, err := investor.New(cfg, w.db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	router := fhttp.NewRouter().WithFlash(security.NewFlash([]byte(appKey), false)).WithRenderer(hview.NewRenderer())
	module.Routes(router.ForModule(module.Name()))
	return module, mounted{router: router, sessions: sessions}
}

// Each page of the area is the signed-in investor's own: what it holds comes
// from the Grant of the session, so two investors reading the same address
// read two different pages, and a person with no investor row reads none.
func TestEveryPageOfTheAreaIsTheSignedInInvestorsOwn(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	bruno := registerPerson(t, w, "user-bruno", cpfBruno)
	contractOver(t, w, ana, 1500)
	_, m := areaOf(t, w, investor.Config{})

	pages := []struct{ path, view, section string }{
		{"/me/withdrawals", investor.ViewWithdrawals, investor.SectionWithdrawals},
		{"/me/contract", investor.ViewContract, investor.SectionContract},
		{"/me/reports", investor.ViewReports, investor.SectionReports},
	}
	for _, page := range pages {
		for _, who := range []struct {
			record *investor.Investor
			id     string
		}{{ana, "user-ana"}, {bruno, "user-bruno"}} {
			if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+page.path, "", m.signIn(t, user(who.id))...); rec.Code != http.StatusOK {
				t.Fatalf("%s answered %d for %s: %s", page.path, rec.Code, who.id, rec.Body)
			}
			if _, ok := pagesDrawn.Load(page.view + "/" + who.record.Name); !ok {
				t.Errorf("%s drew no page for %s", page.path, who.id)
			}
		}
		if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+page.path, "", m.signIn(t, user("user-carol"))...); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d for a person who is no investor, want %d", page.path, rec.Code, http.StatusNotFound)
		}
	}

	contract, _ := pagesDrawn.Load(investor.ViewContract + "/" + ana.Name)
	if got := contract.(investor.ContractPageData).Contracts; len(got) != 1 || got[0].Number == "" {
		t.Errorf("Ana's contract page holds %+v, want her one contract", got)
	}
	other, _ := pagesDrawn.Load(investor.ViewContract + "/" + bruno.Name)
	if got := other.(investor.ContractPageData).Contracts; len(got) != 0 {
		t.Errorf("Bruno's contract page holds %+v, want none: the contract is Ana's", got)
	}
	reports, _ := pagesDrawn.Load(investor.ViewReports + "/" + bruno.Name)
	if page := reports.(investor.ReportsPageData); len(page.ReportMonths)+len(page.ReportYears) != 0 {
		t.Errorf("Bruno has no contract and is offered reports: %+v", page)
	}
	if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+"/me/withdrawals", ""); rec.Code != http.StatusSeeOther {
		t.Errorf("a visitor with no session was answered %d, want the redirect to sign in", rec.Code)
	}
}

// Every page of the area carries the same menu, with its own section marked,
// and the pages the application adds sit where it said.
func TestEveryPageCarriesTheSectionMenuWithItsOwnSectionMarked(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	module, m := areaOf(t, w, investor.Config{Sections: []investor.Section{
		{Key: "billing", Label: "Faturamento", URL: "/investidor/faturamento", After: investor.SectionSummary},
		{Key: "share", Label: "Sua parte", URL: "/investidor/sua-parte", After: "billing"},
		{Key: "last", Label: "Último", URL: "/investidor/ultimo"},
	}})

	var keys []string
	for _, link := range module.SectionLinks(context.Background(), "share") {
		keys = append(keys, link.Key)
		if link.Current != (link.Key == "share") {
			t.Errorf("the section %q is marked current=%v", link.Key, link.Current)
		}
	}
	want := []string{"summary", "billing", "share", "withdrawals", "contract", "reports", "last"}
	if len(keys) != len(want) {
		t.Fatalf("the menu is %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("the menu is %v, want %v", keys, want)
		}
	}
	if label := module.SectionLinks(context.Background(), "summary")[0].Label; label != "Resumo" {
		t.Errorf("the first section reads %q, want Resumo", label)
	}

	if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+"/me/contract", "", m.signIn(t, user("user-ana"))...); rec.Code != http.StatusOK {
		t.Fatalf("the contract page answered %d: %s", rec.Code, rec.Body)
	}
	value, _ := pagesDrawn.Load(investor.ViewContract + "/" + ana.Name)
	page := value.(investor.ContractPageData)
	if page.Section != investor.SectionContract || page.CurrentSection() != "Contrato" || len(page.Sections) != len(want) {
		t.Errorf("the contract page is in section %q (%q) with %d links", page.Section, page.CurrentSection(), len(page.Sections))
	}
}

// The summary says when the next payment is only when the application knows,
// and a failure to know leaves the figure out rather than the page.
func TestTheSummarySaysTheNextPaymentOnlyWhenTheApplicationKnowsIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	contractOver(t, w, ana, 1500)

	for name, tc := range map[string]struct {
		next func(context.Context, security.Subject) (investor.Day, error)
		want string
	}{
		"given":  {func(context.Context, security.Subject) (investor.Day, error) { return "2026-12-20", nil }, "20/12/2026"},
		"none":   {nil, ""},
		"failed": {func(context.Context, security.Subject) (investor.Day, error) { return "", context.DeadlineExceeded }, ""},
	} {
		_, m := areaOf(t, w, investor.Config{NextPayout: tc.next})
		if rec := m.answer(t, http.MethodGet, investor.DefaultPrefix+"/me", "", m.signIn(t, user("user-ana"))...); rec.Code != http.StatusOK {
			t.Fatalf("%s: the summary answered %d: %s", name, rec.Code, rec.Body)
		}
		value, _ := drawn.Load(ana.Name)
		if got := value.(investor.DashboardPageData).NextPayout; got != tc.want {
			t.Errorf("%s: the next payment reads %q, want %q", name, got, tc.want)
		}
	}
}

// A section the menu could not draw, or could not tell from another, is
// refused where the module is wired.
func TestASectionTheMenuCannotDrawIsRefused(t *testing.T) {
	t.Parallel()
	for name, sections := range map[string][]investor.Section{
		"no key":       {{Label: "A", URL: "/a"}},
		"no label":     {{Key: "a", URL: "/a"}},
		"no address":   {{Key: "a", Label: "A"}},
		"twice":        {{Key: "a", Label: "A", URL: "/a"}, {Key: "a", Label: "B", URL: "/b"}},
		"the packages": {{Key: investor.SectionReports, Label: "A", URL: "/a"}},
	} {
		if err := (investor.Config{Sections: sections}).Validate(); err == nil {
			t.Errorf("%s: the configuration was accepted", name)
		}
	}
}
