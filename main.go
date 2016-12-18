package main

import (
	"log"
	"net/http"
	"os"

	"github.com/boltdb/bolt"
	"github.com/gorilla/mux"
)

type Bucket struct {
	Id    int    `json:"id"`
	Title string `json:"title"`
}

type Token struct {
	Bucket int    `json:"bucket"`
	Job    int    `json:"job"`
	Email  string `json:"email"`
}

var fatalLog = log.New(os.Stdout, "FATAL: ", log.LstdFlags)

var db *bolt.DB

func main() {
	var err error
	db, err = bolt.Open("/db/tas-bucket.db", 0644, nil)
	if err != nil {
		fatalLog.Fatal(err)
	}
	defer db.Close()

	r := mux.NewRouter()
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	fatalLog.Fatal(http.ListenAndServe(":8080", r))
}
