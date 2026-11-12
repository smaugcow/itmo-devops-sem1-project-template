package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sizedArchive(t *testing.T, size int) []byte {
	t.Helper()
	prefix := "id,name,category,price,create_date\n1,"
	suffix := ",fruit,10,2024-01-01\n"
	content := prefix + strings.Repeat("a", size-len(prefix)-len(suffix)) + suffix
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	member, err := writer.Create("data.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestArchiveSizeBoundary(t *testing.T) {
	rows, err := readArchive(sizedArchive(t, maxCSVBytes))
	if err != nil || len(rows) != 1 {
		t.Fatalf("exact limit: rows=%d error=%v", len(rows), err)
	}
	oversized := sizedArchive(t, maxCSVBytes+1)
	if rows, err := readArchive(oversized); err == nil || rows != nil {
		t.Fatalf("oversized archive accepted: rows=%d error=%v", len(rows), err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v0/prices", bytes.NewReader(oversized))
	request.Header.Set("Content-Type", "application/zip")
	response := httptest.NewRecorder()
	server{}.upload(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized upload: status=%d", response.Code)
	}
}
