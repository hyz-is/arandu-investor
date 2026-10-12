package investor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A workbook in the Office Open XML format, written with the standard
// library: a zip of a few XML parts. It covers what the reports need -- text,
// money, a bold header -- and nothing more, so the package carries no
// spreadsheet dependency for two downloads.

// cell is one value of a sheet: text, or centavos shown as reais.
type cell struct {
	text   string
	cents  int64
	money  bool
	number bool
	bold   bool
}

func text(s string) cell         { return cell{text: s} }
func bold(s string) cell         { return cell{text: s, bold: true} }
func money(v int64) cell         { return cell{cents: v, money: true} }
func integer(v int64) cell       { return cell{cents: v, number: true} }
func percentBps(v int64) cell    { return text(strconv.FormatFloat(float64(v)/100, 'f', 2, 64) + "%") }
func blankRow() []cell           { return nil }
func rowOf(cells ...cell) []cell { return cells }

// sheet is one tab of a workbook.
type sheet struct {
	name string
	rows [][]cell
}

// The style indexes of styles.xml below: 0 plain, 1 bold, 2 money, 3 integer.
const (
	styleBold    = 1
	styleMoney   = 2
	styleInteger = 3
)

// writeWorkbook writes the sheets as an .xlsx to w.
func writeWorkbook(w io.Writer, sheets []sheet) error {
	z := zip.NewWriter(w)
	part := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, xml.Header+body)
		return err
	}

	var overrides, rels, entries strings.Builder
	for i, sh := range sheets {
		n := i + 1
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		fmt.Fprintf(&entries, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(sheetName(sh.name)), n, n)
	}
	styleRel := len(sheets) + 1

	steps := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			overrides.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			entries.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, styleRel) +
			`</Relationships>`},
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
			`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
			`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
			`<xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs>` +
			`</styleSheet>`},
	}
	for _, s := range steps {
		if err := part(s.name, s.body); err != nil {
			return err
		}
	}
	for i, sh := range sheets {
		if err := part(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)); err != nil {
			return err
		}
	}
	return z.Close()
}

// sheetXML is one worksheet's XML.
func sheetXML(sh sheet) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range sh.rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, v := range row {
			ref := column(c) + strconv.Itoa(r+1)
			switch {
			case v.money:
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, styleMoney, decimal(v.cents))
			case v.number:
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%d</v></c>`, ref, styleInteger, v.cents)
			default:
				style := ""
				if v.bold {
					style = fmt.Sprintf(` s="%d"`, styleBold)
				}
				fmt.Fprintf(&b, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(v.text))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// column is the letters of a zero-based column index: 0 is A, 26 is AA.
func column(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// decimal writes centavos as a plain decimal number: 12345 is 123.45.
func decimal(v int64) string { return cents(v) }

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// sheetName is a name Excel accepts: at most 31 characters, none of []:*?/\.
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if len([]rune(s)) > 31 {
		s = string([]rune(s)[:31])
	}
	return s
}
