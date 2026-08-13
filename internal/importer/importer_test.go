package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func testDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(".", ".test-import-"+time.Now().Format("20060102150405.000000000"))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	return path
}

func TestTextNamesAreTrimmedAndStableDeduplicated(t *testing.T) {
	path := filepath.Join(testDir(t), "names.txt")
	if err := os.WriteFile(path, []byte(" 张三 \n\n李四\n张三\n王五,备注\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := ReadNames(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"张三", "李四", "王五"}
	if len(names) != len(want) {
		t.Fatalf("names = %#v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %#v, want %#v", names, want)
		}
	}
}

func TestXLSXUsesNamedColumn(t *testing.T) {
	path := filepath.Join(testDir(t), "names.xlsx")
	book := excelize.NewFile()
	defer func() { _ = book.Close() }()
	sheet := book.GetSheetName(0)
	_ = book.SetCellValue(sheet, "A1", "编号")
	_ = book.SetCellValue(sheet, "B1", "名称")
	_ = book.SetCellValue(sheet, "A2", "1")
	_ = book.SetCellValue(sheet, "B2", "恒星科技")
	_ = book.SetCellValue(sheet, "A3", "2")
	_ = book.SetCellValue(sheet, "B3", "恒星科技")
	_ = book.SetCellValue(sheet, "B4", "远山基金")
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	names, err := ReadNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(names), 2; got != want || names[0] != "恒星科技" || names[1] != "远山基金" {
		t.Fatalf("names = %#v", names)
	}
}
