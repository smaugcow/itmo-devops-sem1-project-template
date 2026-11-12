package main

import (
	"archive/tar"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pricePattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func validatedArchive(data []byte, archiveType string) ([]price, int64, error) {
	if archiveType == "" || archiveType == "zip" {
		content, err := zipCSV(data)
		if err != nil {
			return nil, 0, err
		}
		return validatedCSV(bytes.NewReader(content))
	}
	if archiveType != "tar" {
		return nil, 0, errors.New("неизвестный тип архива")
	}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err != nil {
			return nil, 0, err
		}
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, ".csv") {
			continue
		}
		if header.Size > maxCSVBytes {
			return nil, 0, errors.New("csv превышает допустимый размер")
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxCSVBytes+1))
		if err != nil {
			return nil, 0, err
		}
		if len(content) > maxCSVBytes {
			return nil, 0, errors.New("csv превышает допустимый размер")
		}
		return validatedCSV(bytes.NewReader(content))
	}
}

func validatedCSV(source io.Reader) ([]price, int64, error) {
	reader := csv.NewReader(source)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, 0, err
	}
	columns := make(map[string]int)
	for i, name := range header {
		columns[strings.TrimSpace(name)] = i
	}
	for _, name := range []string{"id", "name", "category", "price", "create_date"} {
		if _, ok := columns[name]; !ok {
			return nil, 0, fmt.Errorf("нет столбца %s", name)
		}
	}
	var items []price
	var total int64
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		total++
		if err != nil || len(row) != len(header) {
			continue
		}
		item := price{Name: row[columns["name"]], Category: row[columns["category"]], Price: row[columns["price"]]}
		item.ID, err = strconv.ParseInt(row[columns["id"]], 10, 64)
		if err != nil || item.ID <= 0 || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Category) == "" || !pricePattern.MatchString(item.Price) {
			continue
		}
		if len(item.Price) > 1000 {
			continue
		}
		item.Created, err = time.Parse("2006-01-02", row[columns["create_date"]])
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, total, nil
}

func filteredQuery(values url.Values) (string, []any, error) {
	query := "SELECT id, name, category, price, create_date FROM prices"
	var clauses []string
	var args []any
	for _, filter := range []struct{ name, column, operator string }{
		{"start", "create_date", ">="}, {"end", "create_date", "<="}, {"min", "price", ">="}, {"max", "price", "<="},
	} {
		raw, exists := values[filter.name]
		if !exists {
			continue
		}
		if len(raw) != 1 || raw[0] == "" {
			return "", nil, errors.New("пустой или повторный фильтр")
		}
		value := raw[0]
		if filter.column == "create_date" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return "", nil, err
			}
		} else {
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number <= 0 {
				return "", nil, errors.New("цена должна быть положительным целым")
			}
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s %s $%d", filter.column, filter.operator, len(args)))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	return query + " ORDER BY id, create_date, name", args, nil
}
