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
	"sort"
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

type Buckets []Bucket

func (slice Buckets) Len() int {
	return len(slice)
}

func (slice Buckets) Less(i, j int) bool {
	return slice[i].Position < slice[j].Position
}

func (slice Buckets) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

type Token struct {
	Id     int    `json:"id"`
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
	Id:       0,
	Title:    "Applied",
	Position: 0,
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
			decoder := json.NewDecoder(getOneFromBolt(bid, getTenantBucket(req.Header.Get("tazzy-tenant"))))
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
	infoLog.Printf("UpdateBucket strconv error: %v", err)

	var bucket Bucket
	bucket = Bucket{
		Id:    bid,
		Title: req.FormValue("Title"),
	}
	infoLog.Printf("Updatebucket bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantBucket(tenant))
		// Check if this is a new bucket
		if bid == 0 {
			var buckets Buckets
			decoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
			infoLog.Printf("UpdateBucket json error: %v", decoder.Decode(&buckets))

			id, _ := b.NextSequence()
			bucket.Id = int(id)
			bucket.Position = buckets.Len() + 1
		}
		data, err := json.Marshal(&bucket)
		if err == nil {
			return b.Put(itob(bucket.Id), data)
		}
		return err
	}))
	basePage(rw, req)
}

func remove(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	bid, err := strconv.Atoi(vars["bucket"])
	infoLog.Printf("Remove strconv error: %v", err)
	infoLog.Printf("Remove bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		var buckets Buckets
		decoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
		infoLog.Printf("Remove json error: %v", decoder.Decode(&buckets))
		sort.Sort(buckets)
		b := tx.Bucket(getTenantBucket(req.Header.Get("tazzy-tenant")))
		var position int
		for _, bucket := range buckets {
			if bucket.Id == bid {
				position = bucket.Position
				b.Delete(itob(bid))
			} else if position != 0 && bucket.Position > position {
				bucket.Position--
				data, _ := json.Marshal(&bucket)
				b.Put(itob(bucket.Id), data)
			}
		}
		return nil
	}))
	basePage(rw, req)
}

func toRight(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	bid, err := strconv.Atoi(vars["bucket"])
	infoLog.Printf("Move strconv error: %v", err)
	infoLog.Printf("Move bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		var buckets Buckets
		decoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
		infoLog.Printf("Move json error: %v", decoder.Decode(&buckets))
		if buckets.Len() == 0 {
			return nil
		}
		sort.Sort(buckets)
		b := tx.Bucket(getTenantBucket(req.Header.Get("tazzy-tenant")))
		for i, bucket := range buckets[:buckets.Len()-1] {
			if bucket.Id == bid {
				bucket.Position++
				data, _ := json.Marshal(&bucket)
				b.Put(itob(bid), data)
				buckets[i+1].Position--
				data, _ = json.Marshal(&buckets[i+1])
				b.Put(itob(buckets[i+1].Id), data)
				break
			} else if i == buckets.Len()-1 {
				infoLog.Printf("Cannot move beyond end position")
			}
		}
		return nil
	}))
	basePage(rw, req)
}

func toLeft(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	bid, err := strconv.Atoi(vars["bucket"])
	infoLog.Printf("Move strconv error: %v", err)
	infoLog.Printf("Move bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		var buckets Buckets
		decoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
		infoLog.Printf("Move json error: %v", decoder.Decode(&buckets))
		sort.Sort(buckets)
		b := tx.Bucket(getTenantBucket(req.Header.Get("tazzy-tenant")))
		for i, bucket := range buckets[1:] {
			if bucket.Id == bid && bucket.Position == 1 {
				infoLog.Printf("Cannot below base position")
				break
			} else if bucket.Id == bid {
				bucket.Position--
				data, _ := json.Marshal(&bucket)
				b.Put(itob(bid), data)
				buckets[i].Position++
				data, _ = json.Marshal(&buckets[i])
				b.Put(itob(buckets[i].Id), data)
				break
			}
		}
		return nil
	}))
	basePage(rw, req)
}

func apply(rw http.ResponseWriter, req *http.Request) {
	var token Token
	err := json.NewDecoder(req.Body).Decode(&token)
	if err != nil {
		panic(err)
	}
	defer req.Body.Close()
	tenant := req.Header.Get("tazzy-tenant")
	infoLog.Printf("Updatebucket bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantCandidate(tenant))
		id, _ := b.NextSequence()
		token.Id = int(id)
		data, err := json.Marshal(&token)
		if err == nil {
			return b.Put(itob(token.Id), data)
		}
		return err
	}))
	rw.WriteHeader(200)
}

func advance(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	tid, err := strconv.Atoi(vars["token"])
	infoLog.Printf("Advance strconv error: %v", err)

	var token Token
	decoder := json.NewDecoder(getOneFromBolt(tid, getTenantCandidate(req.Header.Get("tazzy-tenant"))))
	infoLog.Printf("Advance json error: %v", decoder.Decode(&token))

	var buckets Buckets
	bucketDecoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
	infoLog.Printf("Advance Bucket json error: %v", bucketDecoder.Decode(&buckets))

	var position int
	for _, bucket := range buckets {
		if bucket.Id == token.Bucket {
			position = bucket.Position
			break
		}
	}

	var newId int
	for _, bucket := range buckets {
		if bucket.Position == position+1 {
			newId = bucket.Id
		}
	}

	if newId == 0 {
		http.Redirect(rw, req, "/remove/token/{token}", 301)
		return
	}

	infoLog.Printf("Advance bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantCandidate(req.Header.Get("tazzy-tenant")))
		token.Bucket = newId
		data, err := json.Marshal(&token)
		if err == nil {
			return b.Put(itob(token.Id), data)
		}
		return err
	}))
	http.Redirect(rw, req, "/", 301)
}

func removeToken(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	tid, err := strconv.Atoi(vars["token"])
	infoLog.Printf("Remove strconv error: %v", err)
	infoLog.Printf("Remove bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(getTenantCandidate(req.Header.Get("tazzy-tenant")))
		b.Delete(itob(tid))
		return nil
	}))
	http.Redirect(rw, req, "/", 301)
}

func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func getOneFromBolt(id int, bucket []byte) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		buffer.Write(b.Get(itob(id)))
		return nil
	})
	return buffer
}

func getListFromBolt(bucket []byte) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
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
	var buckets Buckets
	decoder := json.NewDecoder(getListFromBolt(getTenantBucket(req.Header.Get("tazzy-tenant"))))
	infoLog.Printf("BasePage Bucket json error: %v", decoder.Decode(&buckets))

	var tokens []Token
	tokenDecoder := json.NewDecoder(getListFromBolt(getTenantCandidate(req.Header.Get("tazzy-tenant"))))
	infoLog.Printf("BasePage Token json error: %v", tokenDecoder.Decode(&tokens))

	t, err := template.ParseFiles("static/index.html")
	infoLog.Printf("BasePage template error: %v", err)
	if buckets == nil {
		buckets = appliedBucket
	} else {
		buckets = append(appliedBucket, buckets...)
	}
	sort.Sort(buckets)
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
	r.HandleFunc("/tas/devs/allan/submit", apply)
	r.HandleFunc("/advance/{token}", advance)
	r.HandleFunc("/create/{bucket}", create)
	r.HandleFunc("/remove/bucket/{bucket}", remove)
	r.HandleFunc("/remove/token/{token}", removeToken)
	r.HandleFunc("/move/{bucket}/toLeft", toLeft)
	r.HandleFunc("/move/{bucket}/toRight", toRight)
	r.HandleFunc("/tas/core/tenants", newTenant)
	r.HandleFunc("/tas/core/tenants/{tenant}", deleteTenant)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	fatalLog.Fatal(http.ListenAndServe(":8080", r))
}
