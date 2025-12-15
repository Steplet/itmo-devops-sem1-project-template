package handlers

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"project_sem/internal/model"
)

func PostPrice(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		rawData, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Errorf("read body: %w", err).Error(), http.StatusBadRequest)
			return
		}

		zr, err := zip.NewReader(bytes.NewReader(rawData), int64(len(rawData)))
		if err != nil {
			http.Error(w, fmt.Errorf("open new zip reader: %w", err).Error(), http.StatusBadRequest)
			return
		}

		var csvFile *zip.File
		for _, f := range zr.File {
			fileNames := strings.Split(f.Name, "/")
			nameCsv := fileNames[len(fileNames)-1]

			if strings.HasSuffix(nameCsv, ".csv") {
				csvFile = f
				break
			}
		}

		if csvFile == nil {
			http.Error(w, "CSV file not found in archive", http.StatusBadRequest)
			return
		}

		rc, err := csvFile.Open()
		if err != nil {
			http.Error(w, fmt.Errorf("open file in zip: %w", err).Error(), http.StatusBadRequest)
			return
		}

		defer rc.Close()

		reader := csv.NewReader(rc)

		// skip first line
		_, err = reader.Read()
		if err != nil {
			http.Error(w, fmt.Errorf("read first line: %w", err).Error(), http.StatusBadRequest)
			return
		}
		arrayOfData, err := readData(reader)
		if err != nil {
			http.Error(w, fmt.Errorf("read data: %w", err).Error(), http.StatusBadRequest)
		}
		data, err := insertData(db, arrayOfData)
		if err != nil {
			http.Error(w, fmt.Errorf("insert data in db: %w", err).Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(data)
		if err != nil {
			http.Error(w, fmt.Errorf("encode json: %w", err).Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "file not found", http.StatusNotFound)
	}
}

func readData(reader *csv.Reader) (*[]PriceRecord, error) {
	var records []PriceRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		record, err := parseAndValidateRow(row)
		if err != nil {
			return nil, fmt.Errorf("parse row: %w", err)
		}
		records = append(records, record)
	}

	return &records, nil
}

func parseAndValidateRow(row []string) (PriceRecord, error) {
	if len(row) < 5 {
		return PriceRecord{}, fmt.Errorf("invalid row length: %d", len(row))
	}

	price, err := strconv.ParseFloat(row[3], 64)
	if err != nil {
		return PriceRecord{}, fmt.Errorf("invalid price: %w", err)
	}

	return PriceRecord{
		Name:      row[1],
		Category:  row[2],
		Price:     price,
		CreatedAt: row[4],
	}, nil
}

func insertData(db *sql.DB, records *[]PriceRecord) (*ResponseStats, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	query, err := tx.Prepare(`
		INSERT INTO prices (create_date, name, category, price) 
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare statement: %w", err)
	}
	defer query.Close()

	for _, record := range *records {
		_, err = query.Exec(
			record.CreatedAt,
			record.Name,
			record.Category,
			record.Price,
		)
		if err != nil {
			return nil, fmt.Errorf("insert record: %w", err)
		}
	}

	stats := ResponseStats{TotalItems: len(*records)}

	err = tx.QueryRow(`
		SELECT 
			COALESCE(SUM(price), 0) as total_price,
			COUNT(DISTINCT category) as total_categories
		FROM prices
	`).Scan(&stats.TotalPrice, &stats.TotalCategories)
	if err != nil {
		return nil, fmt.Errorf("calculate statistics: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &stats, nil
}

func GetPrice(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		var prices []model.Price

		rows, err := db.Query("SELECT id, create_date, name, category, price FROM prices")
		if err != nil {
			http.Error(w, fmt.Errorf("query db: %w", err).Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var price model.Price
			err := rows.Scan(&price.ID, &price.CreatedAt, &price.Name, &price.Category, &price.Price)
			if err != nil {
				http.Error(w, fmt.Errorf("scan row: %w", err).Error(), http.StatusInternalServerError)
				return
			}
			prices = append(prices, price)
		}

		if err := rows.Err(); err != nil {
			http.Error(w, fmt.Errorf("rows error: %w", err).Error(), http.StatusInternalServerError)
			return
		}

		var csvBuffer bytes.Buffer
		csvWriter := csv.NewWriter(&csvBuffer)

		err = csvWriter.Write([]string{"id", "created_at", "name", "category", "price"})
		if err != nil {
			http.Error(w, fmt.Errorf("write csv header: %w", err).Error(), http.StatusInternalServerError)
			return
		}

		for _, price := range prices {
			err = csvWriter.Write([]string{
				strconv.FormatInt(price.ID, 10),
				price.CreatedAt,
				price.Name,
				price.Category,
				strconv.FormatFloat(price.Price, 'f', -1, 64),
			})
			if err != nil {
				http.Error(w, fmt.Errorf("write csv row: %w", err).Error(), http.StatusInternalServerError)
				return
			}
		}

		csvWriter.Flush()

		sendZip(csvBuffer.Bytes(), w)

	}
}

func sendZip(csvFile []byte, w http.ResponseWriter) {

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"prices.zip\"")

	zw := zip.NewWriter(w)
	defer zw.Close()

	zf, err := zw.Create("data.csv")
	if err != nil {
		http.Error(w, fmt.Errorf("create zip output file: %w", err).Error(), http.StatusBadRequest)
		return
	}

	_, err = zf.Write(csvFile)
	if err != nil {
		http.Error(w, fmt.Errorf("write output file: %w", err).Error(), http.StatusBadRequest)
		return
	}

}
