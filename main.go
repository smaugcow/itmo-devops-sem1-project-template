package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const maxCSVBytes = 32 << 20

type price struct {
	ID       int64
	Name     string
	Category string
	Price    string
	Created  time.Time
}

type statistics struct {
	TotalCount      int64       `json:"total_count"`
	DuplicatesCount int64       `json:"duplicates_count"`
	TotalItems      int64       `json:"total_items"`
	TotalCategories int64       `json:"total_categories"`
	TotalPrice      json.Number `json:"total_price"`
}

type server struct{ db *sql.DB }

func setting(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func archiveBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, err
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(file)
	}
	return io.ReadAll(r.Body)
}

func readArchive(data []byte) ([]price, error) {
	content, err := zipCSV(data)
	if err != nil {
		return nil, err
	}
	return readCSV(bytes.NewReader(content))
}

func zipCSV(data []byte) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !strings.HasSuffix(file.Name, ".csv") {
			continue
		}
		if file.UncompressedSize64 > maxCSVBytes {
			return nil, errors.New("csv превышает допустимый размер")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		content, err := io.ReadAll(io.LimitReader(stream, maxCSVBytes+1))
		if err != nil {
			return nil, err
		}
		if len(content) > maxCSVBytes {
			return nil, errors.New("csv превышает допустимый размер")
		}
		return content, nil
	}
	return nil, errors.New("в архиве нет csv")
}

func readCSV(source io.Reader) ([]price, error) {
	items, _, err := validatedCSV(source)
	return items, err
}

func (s server) upload(w http.ResponseWriter, r *http.Request) {
	body, err := archiveBody(w, r)
	if err != nil {
		http.Error(w, "не удалось прочитать архив", http.StatusBadRequest)
		return
	}
	archiveType := r.URL.Query().Get("type")
	items, total, err := validatedArchive(body, archiveType)
	if err != nil {
		http.Error(w, "некорректный архив или csv", http.StatusBadRequest)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "база недоступна", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	result := statistics{TotalCount: total}
	for _, item := range items {
		inserted, err := tx.ExecContext(r.Context(), "INSERT INTO prices(id, name, category, price, create_date) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING", item.ID, item.Name, item.Category, item.Price, item.Created)
		if err != nil {
			http.Error(w, "не удалось сохранить данные", http.StatusInternalServerError)
			return
		}
		count, err := inserted.RowsAffected()
		if err != nil {
			http.Error(w, "не удалось проверить запись", 500)
			return
		}
		result.TotalItems += count
		if count == 0 {
			result.DuplicatesCount++
		}
	}
	err = tx.QueryRowContext(r.Context(), "SELECT COUNT(DISTINCT category), COALESCE(SUM(price), 0) FROM prices").Scan(&result.TotalCategories, &result.TotalPrice)
	if err != nil {
		http.Error(w, "не удалось прочитать итог", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "не удалось сохранить данные", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Print(err)
	}
}

func (s server) download(w http.ResponseWriter, r *http.Request) {
	query, args, err := filteredQuery(r.URL.Query())
	if err != nil {
		http.Error(w, "некорректный фильтр", 400)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		http.Error(w, "не удалось прочитать данные", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	file, err := archive.Create("data.csv")
	if err != nil {
		http.Error(w, "не удалось создать архив", http.StatusInternalServerError)
		return
	}
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"id", "name", "category", "price", "create_date"}); err != nil {
		http.Error(w, "не удалось записать csv", 500)
		return
	}
	for rows.Next() {
		var item price
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Price, &item.Created); err != nil {
			http.Error(w, "не удалось прочитать строку", 500)
			return
		}
		if err := writer.Write([]string{strconv.FormatInt(item.ID, 10), item.Name, item.Category, item.Price, item.Created.Format("2006-01-02")}); err != nil {
			http.Error(w, "не удалось записать csv", 500)
			return
		}
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "не удалось прочитать строки", 500)
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		http.Error(w, "не удалось записать csv", 500)
		return
	}
	if err := archive.Close(); err != nil {
		http.Error(w, "не удалось закрыть архив", 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="data.zip"`)
	if _, err := w.Write(data.Bytes()); err != nil {
		log.Print(err)
	}
}

func main() {
	connection := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=%s", setting("POSTGRES_HOST", "localhost"), setting("POSTGRES_PORT", "5432"), setting("POSTGRES_DB", "project-sem-1"), setting("POSTGRES_USER", "validator"), setting("POSTGRES_PASSWORD", "val1dat0r"), setting("POSTGRES_SSLMODE", "disable"))
	db, err := sql.Open("postgres", connection)
	if err != nil {
		log.Fatal("не удалось открыть подключение")
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal("база недоступна")
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS prices (id BIGINT NOT NULL, name TEXT NOT NULL, category TEXT NOT NULL, price NUMERIC NOT NULL, create_date DATE NOT NULL, UNIQUE(id, name, category, price, create_date))")
	if err != nil {
		log.Fatal("не удалось подготовить таблицу")
	}
	app := server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/prices", app.upload)
	mux.HandleFunc("GET /api/v0/prices", app.download)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "база недоступна", 503)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	listener := &http.Server{Addr: ":" + setting("PORT", "8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(listener.ListenAndServe())
}
