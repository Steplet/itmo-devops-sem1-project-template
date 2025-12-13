package main

import (
	"log"
	"net/http"

	"project_sem/internal/db"
	"project_sem/internal/handlers"
)

func main() {
	dataBase, err := db.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer dataBase.Close()

	http.HandleFunc("POST /api/v0/prices", handlers.PostPrice(dataBase))
	http.HandleFunc("GET /api/v0/prices", handlers.GetPrice(dataBase))

	log.Println("Listening on port 8080")
	http.ListenAndServe(":8080", nil)
}
