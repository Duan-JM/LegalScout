// Package importer reads a deliberately small and auditable name-list format.
package importer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// ReadNames recognises .xlsx first-column data or a column titled 名称/name/
// 姓名/主体. Text files remain supported for existing users.
func ReadNames(path string) ([]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx":
		return readXLSX(path)
	case ".txt", ".csv", "":
		return readText(path)
	default:
		return nil, fmt.Errorf("不支持的名单格式 %q；请使用 .xlsx 或 .txt", filepath.Ext(path))
	}
}

func normalize(value string) string {
	return strings.TrimSpace(strings.TrimFunc(value, unicode.IsSpace))
}

func stableUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalize(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func readText(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return scan(file)
}

func scan(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var names []string
	for scanner.Scan() {
		// CSV users commonly put a single value in the first column.
		names = append(names, strings.Split(scanner.Text(), ",")[0])
	}
	return stableUnique(names), scanner.Err()
}

func readXLSX(path string) ([]string, error) {
	book, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Excel: %w", err)
	}
	defer func() { _ = book.Close() }()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("Excel 中没有工作表")
	}
	rows, err := book.GetRows(sheets[0])
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("读取 Excel 首个工作表: %w", err)
	}
	column := 0
	start := 0
	for idx, cell := range rows[0] {
		switch strings.ToLower(normalize(cell)) {
		case "名称", "姓名", "主体", "name", "entity":
			column, start = idx, 1
		}
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows[start:] {
		if column < len(row) {
			values = append(values, row[column])
		}
	}
	return stableUnique(values), nil
}
