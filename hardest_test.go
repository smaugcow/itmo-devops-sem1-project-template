package main

import (
	"archive/tar"
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestValidatedRows(t *testing.T) {
	data := "id,name,category,price,create_date\n1,a,c,1.20,2024-02-29\n2,,c,2,2024-01-01\n3,b,c,NaN,2024-01-01\n4,b,c,2,2023-02-29\n5,b\n1,a,c,1.20,2024-02-29\n"
	rows, total, err := validatedCSV(strings.NewReader(data))
	if err != nil || total != 6 || len(rows) != 2 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "data.csv", Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rows, total, err = validatedArchive(buffer.Bytes(), "tar")
	if err != nil || total != 6 || len(rows) != 2 {
		t.Fatalf("tar rows=%d total=%d err=%v", len(rows), total, err)
	}
	if _, _, err = validatedArchive(buffer.Bytes(), "rar"); err == nil {
		t.Fatal("unsupported archive accepted")
	}
}

func TestFilters(t *testing.T) {
	query, args, err := filteredQuery(url.Values{"start": {"2024-01-01"}, "end": {"2024-01-31"}, "min": {"300"}, "max": {"1000"}})
	if err != nil || len(args) != 4 || !strings.Contains(query, "price >= $3 AND price <= $4") {
		t.Fatalf("query=%s args=%v err=%v", query, args, err)
	}
	for _, values := range []url.Values{{"min": {"0"}}, {"min": {"1.5"}}, {"start": {"2024-02-30"}}, {"max": {"1", "2"}}, {"end": {""}}} {
		if _, _, err := filteredQuery(values); err == nil {
			t.Fatalf("invalid filter accepted: %v", values)
		}
	}
}
