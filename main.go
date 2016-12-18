package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/boltdb/bolt"
	"github.com/gorilla/mux"
)

type BucketTokens struct {
	Bucket Bucket  `json:"bucket"`
	Tokens []Token `json:"tokens"`
}

type Bucket struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

type Token struct {
	Bucket int    `json:"bucket"`
	Job    int    `json:"job"`
	Email  string `json:"email"`
}

type TenantInfo struct {
	ShortCode string `json:"shortCode"`
}

var fatalLog = log.New(os.Stdout, "FATAL: ", log.LstdFlags)
var infoLog = log.New(os.Stdout, "INFO: ", log.LstdFlags)

var db *bolt.DB

var appliedBucket = []Bucket{Bucket{
	Id:    0,
	Title: "Applied",
}}

func getTenantBucket(tenant string) []byte {
	return []byte(fmt.Sprintf("%s-Buckets", tenant))
}

func getTenantCandidate(tenant string) []byte {
	return []byte(fmt.Sprintf("%s-Candidates", tenant))
}

func newTenant(rw http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var info TenantInfo
	infoLog.Printf("NewTenant json error: %v", decoder.Decode(&info))
	infoLog.Printf("NewTenant bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket(getTenantBucket(info.ShortCode))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucket(getTenantCandidate(info.ShortCode))
		return err
	}))
}

func deleteTenant(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	infoLog.Printf("DeleteTenant bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		err := tx.DeleteBucket(getTenantBucket(vars["tenant"]))
		if err != nil {
			return err
		}
		return tx.DeleteBucket(getTenantCandidate(vars["tenant"]))
	}))
}

func create(rw http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		updateBucket(rw, req)
	} else {
		vars := mux.Vars(req)
		t, err := template.ParseFiles("static/create.html")
		infoLog.Printf("Create template error: %v", err)
		if vars["bucket"] == "0" {
			t.Execute(rw, Bucket{})
		} else {
			bid, err := strconv.Atoi(vars["bucket"])
			infoLog.Printf("Create strconv error: %v", err)
			decoder := json.NewDecoder(getBucketFromBolt(bid, req.Header.Get("tazzy-tenant")))
			var bucket Bucket
			infoLog.Printf("Create json error: %v", decoder.Decode(&bucket))
			t.Execute(rw, bucket)
		}
	}
}

func updateBucket(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	err := req.ParseForm()
	if err != nil {
		return
	}
	tenant := req.Header.Get("tazzy-tenant")
	bid, err := strconv.Atoi(vars["bucket"])
	infoLog.Printf("UpdateJob strconv error: %v", err)
	var bucket Bucket
	bucket = Bucket{
		Id:    bid,
		Title: req.FormValue("Title"),
	}
	infoLog.Printf("Updatebucket bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantBucket(tenant))
		// Check if this is a new bucket
		if bid == 0 {
			id, _ := b.NextSequence()
			bucket.Id = int(id)
		}
		data, err := json.Marshal(&bucket)
		if err == nil {
			return b.Put(itob(bucket.Id), data)
		}
		return err
	}))
	http.Redirect(rw, req, fmt.Sprintf("/bucket/%v", bucket.Id), 301)
}

func remove(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	bid, err := strconv.Atoi(vars["bucket"])
	infoLog.Printf("Remove strconv error: %v", err)
	infoLog.Printf("Remove bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantBucket(req.Header.Get("tazzy-tenant")))
		return b.Delete(itob(bid))
	}))
	http.Redirect(rw, req, "/", 301)
}

func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func getBucketFromBolt(bid int, tenant string) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantBucket(tenant))
		buffer.Write(b.Get(itob(bid)))
		return nil
	})
	return buffer
}

func getBucketList(tenant string) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(getTenantBucket(tenant)).Cursor()
		buffer.WriteString("[")
		k, v := c.First()
		if k != nil {
			buffer.Write(v)
			for k, v := c.Next(); k != nil; k, v = c.Next() {
				buffer.WriteString(",")
				buffer.Write(v)
			}
		}
		buffer.WriteString("]")
		return nil
	})
	return buffer
}

func getTokensInBucket(tenant string) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(getTenantBucket(tenant)).Cursor()
		buffer.WriteString("[")
		k, v := c.First()
		if k != nil {
			buffer.Write(v)
			for k, v := c.Next(); k != nil; k, v = c.Next() {
				buffer.WriteString(",")
				buffer.Write(v)
			}
		}
		buffer.WriteString("]")
		return nil
	})
	return buffer
}

func basePage(rw http.ResponseWriter, req *http.Request) {
	var buckets []Bucket
	decoder := json.NewDecoder(getBucketList(req.Header.Get("tazzy-tenant")))
	infoLog.Printf("BasePage Bucket json error: %v", decoder.Decode(&buckets))

	var tokens []Token
	tokenDecoder := json.NewDecoder(getTokensInBucket(req.Header.Get("tazzy-tenant")))
	infoLog.Printf("BasePage Token json error: %v", tokenDecoder.Decode(&tokens))

	t, err := template.ParseFiles("static/index.html")
	infoLog.Printf("BasePage template error: %v", err)
	if buckets == nil {
		buckets = appliedBucket
	} else {
		buckets = append(appliedBucket, buckets...)
	}
	data := []BucketTokens{}
	for _, b := range buckets {
		bTokens := []Token{}
		for _, t := range tokens {
			if t.Bucket == b.Id {
				bTokens = append(bTokens, t)
			}
		}
		data = append(data, BucketTokens{
			Bucket: b,
			Tokens: bTokens,
		})
	}
	t.Execute(rw, data)
}

func main() {
	var err error
	db, err = bolt.Open("/db/tas-bucket.db", 0644, nil)
	if err != nil {
		fatalLog.Fatal(err)
	}
	defer db.Close()

	r := mux.NewRouter()
	r.HandleFunc("/", basePage)
	r.HandleFunc("/create/{bucket}", create)
	r.HandleFunc("/remove/{bucket}", remove)
	r.HandleFunc("/tas/core/tenants", newTenant)
	r.HandleFunc("/tas/core/tenants/{tenant}", deleteTenant)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	fatalLog.Fatal(http.ListenAndServe(":8080", r))
}
