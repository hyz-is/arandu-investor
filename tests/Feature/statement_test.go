package feature_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	investor "github.com/hyz-is/arandu-investor"
)

func TestTheStatementShowsEachLinesShareOfTheCosts(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	monthlyCost(t, w, "servidores", 600000)

	// 20% of the revenue is costs: R$ 6.000 over R$ 30.000.
	record(t, w, "sale-50", investor.KindSale, "loccrm", "2026-10-05", 5000, 50)
	record(t, w, "sale-rest", investor.KindSale, "loccrm", "2026-10-06", 2995000, 29950)
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("closing: %v", err)
	}

	st, err := w.svc.StatementOf(ctx, user("user-ana"), c.ID, investor.StatementFilter{From: "2026-10-05", To: "2026-10-05"})
	if err != nil {
		t.Fatalf("Ana's statement: %v", err)
	}
	if len(st.Lines) != 1 {
		t.Fatalf("the filter by day left %d lines, want the R$ 50 sale", len(st.Lines))
	}
	line := st.Lines[0]
	if line.Cost != 1000 || line.Net != 3950 || st.Open[line.Entry.ID] {
		t.Fatalf("the R$ 50 line: cost %d net %d estimate %v; want 1000, 3950, settled", line.Cost, line.Net, st.Open[line.Entry.ID])
	}

	// Somebody else reads none of it.
	if _, err := w.svc.StatementOf(ctx, user("user-bruno"), c.ID, investor.StatementFilter{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else read Ana's statement: %v", err)
	}
}

func TestTheReportsAreSpreadsheetsWithTheInvoicesInThem(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 3000000, 30000)
	monthlyCost(t, w, "servidores", 600000)
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("closing: %v", err)
	}

	var monthly, annual bytes.Buffer
	if err := w.svc.MonthlyReport(ctx, user("user-ana"), ana.ID, 2026, 10, &monthly); err != nil {
		t.Fatalf("the monthly report: %v", err)
	}
	if err := w.svc.AnnualReport(ctx, user("user-ana"), ana.ID, 2026, &annual); err != nil {
		t.Fatalf("the annual report: %v", err)
	}
	for name, book := range map[string]*bytes.Buffer{"monthly": &monthly, "annual": &annual} {
		sheets := readWorkbook(t, book.Bytes())
		if !strings.Contains(sheets, "3555.00") {
			t.Errorf("the %s report does not carry the invoice's R$ 3.555,00", name)
		}
		if !strings.Contains(sheets, "529.982.247-25") {
			t.Errorf("the %s report does not name the investor's CPF", name)
		}
	}
	if !strings.Contains(readWorkbook(t, annual.Bytes()), "Imposto retido") {
		t.Error("the income report has no withheld-tax column")
	}

	if err := w.svc.AnnualReport(ctx, user("user-bruno"), ana.ID, 2026, io.Discard); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else downloaded Ana's income report: %v", err)
	}
}

// readWorkbook opens an .xlsx as a spreadsheet program would and answers the
// text of its sheets, failing on a part a program would refuse.
func readWorkbook(t *testing.T, body []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the report is not a zip: %v", err)
	}
	parts := map[string]bool{}
	var sheets strings.Builder
	for _, f := range z.File {
		parts[f.Name] = true
		if strings.HasPrefix(f.Name, "xl/worksheets/") {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			_ = r.Close()
			sheets.Write(b)
		}
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		if !parts[required] {
			t.Fatalf("the workbook has no %s", required)
		}
	}
	return sheets.String()
}
