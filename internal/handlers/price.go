package handlers

import (
	"archive/zip"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"project_sem/internal/model"
)

func PostPrice(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		tmpFile, err := createAndCopyTempFile(r.Body)

		if err != nil {
			http.Error(w, fmt.Errorf("create and copy: %w", err).Error(), http.StatusBadRequest)
			return
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		zr, err := zip.OpenReader(tmpFile.Name())
		if err != nil {
			http.Error(w, fmt.Errorf("open zip reader: %w", err).Error(), http.StatusBadRequest)
			return
		}
		defer zr.Close()

		for _, f := range zr.File {
			fileNames := strings.Split(f.Name, "/")
			nameCsv := fileNames[len(fileNames)-1]

			fmt.Printf("nameCsv: %s\n", nameCsv)
			if !strings.HasSuffix(nameCsv, ".csv") {
				continue
			}

			rc, err := f.Open()
			if err != nil {
				http.Error(w, fmt.Errorf("open file in zip: %w", err).Error(), http.StatusBadRequest)
				return
			}

			reader := csv.NewReader(rc)

			// skip first line
			_, err = reader.Read()
			if err != nil {
				http.Error(w, fmt.Errorf("read first line: %w", err).Error(), http.StatusBadRequest)
				return
			}
			data, err := insertData(reader, db)
			if err != nil {
				http.Error(w, fmt.Errorf("insert data in db: %w", err).Error(), http.StatusBadRequest)
				return
			}

			rc.Close()

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(data)
			if err != nil {
				http.Error(w, fmt.Errorf("encode json: %w", err).Error(), http.StatusBadRequest)
				return
			}

			return
		}

		http.Error(w, "file not found", http.StatusNotFound)
	}
}

func createAndCopyTempFile(requestBody io.ReadCloser) (*os.File, error) {
	tmpFile, err := os.CreateTemp("", "upload-*.zip")
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(tmpFile, requestBody)
	if err != nil {
		return nil, err
	}
	tmpFile.Seek(0, 0)

	return tmpFile, nil
}

func insertData(reader *csv.Reader, db *sql.DB) (*ResponseStats, error) {
	var totalItems int
	var categoriesSet = make(map[string]bool)
	var totalPrice float64

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		id, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("pars int: %w", err)
		}

		price, err := strconv.ParseFloat(row[3], 64)
		if err != nil {
			return nil, fmt.Errorf("pars float: %w", err)
		}

		name := row[1]
		category := row[2]
		createdAt := row[4]

		categoriesSet[category] = true
		totalItems++
		totalPrice += price

		_, err = tx.Exec(
			`INSERT INTO prices (id, created_at, name, category, price)
                     VALUES ($1, $2, $3, $4, $5)`,
			id, createdAt, name, category, price,
		)
		if err != nil {
			err := tx.Rollback()
			if err != nil {
				return nil, fmt.Errorf("rollback transaction: %w", err)
			}
			return nil, fmt.Errorf("inserting data: %w", err)
		}
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return &ResponseStats{
		TotalItems:      totalItems,
		TotalCategories: len(categoriesSet),
		TotalPrice:      totalPrice,
	}, nil
}

func GetPrice(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id, created_at, name, category, price FROM prices")
		if err != nil {
			http.Error(w, fmt.Errorf("query db: %w", err).Error(), http.StatusBadRequest)
			return
		}
		defer rows.Close()

		csvWriter, csvFile, err := getCsvWriter()
		if err != nil {
			http.Error(w, fmt.Errorf("csv writer: %w", err).Error(), http.StatusBadRequest)
			return
		}
		defer csvFile.Close()

		for rows.Next() {
			var p model.Price
			err := rows.Scan(&p.ID, &p.CreatedAt, &p.Name, &p.Category, &p.Price)
			if err != nil {
				http.Error(w, fmt.Errorf("scan row: %w", err).Error(), http.StatusBadRequest)
				return
			}

			csvWriter.Write([]string{
				strconv.FormatInt(p.ID, 10),
				p.CreatedAt,
				p.Name,
				p.Category,
				strconv.FormatFloat(p.Price, 'f', -1, 64),
			})
		}
		csvWriter.Flush()

		sendZip(csvFile, w)

	}
}

func getCsvWriter() (*csv.Writer, *os.File, error) {
	csvFile, err := os.CreateTemp("", "data-*.csv")
	if err != nil {
		return nil, nil, err
	}

	csvWriter := csv.NewWriter(csvFile)

	return csvWriter, csvFile, nil
}

func sendZip(csvFile *os.File, w http.ResponseWriter) {

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"prices.zip\"")

	zw := zip.NewWriter(w)
	defer zw.Close()

	zf, err := zw.Create("data.csv")
	if err != nil {
		http.Error(w, fmt.Errorf("create zip output file: %w", err).Error(), http.StatusBadRequest)
		return
	}
	_, err = csvFile.Seek(0, 0)
	if err != nil {
		http.Error(w, fmt.Errorf("seek zip output file: %w", err).Error(), http.StatusBadRequest)
		return
	}

	_, err = io.Copy(zf, csvFile)
	if err != nil {
		http.Error(w, fmt.Errorf("copy zip output file: %w", err).Error(), http.StatusBadRequest)
		return
	}

}
