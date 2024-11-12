package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

var storageHost = os.Getenv("HOST")
var storageUsername = os.Getenv("USERNAME")
var storagePassword = os.Getenv("PASSWORD")

// на данный момент совпадает с дефолтом OpenSearch, когда не передается маппинг
var defaultMapping = strings.NewReader(`{"settings": {"index": {"number_of_shards": 1, "number_of_replicas": 1}}}`)

type DB struct {
	ctx    context.Context
	client *opensearchapi.Client
}

type Document struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
	TS      int64  `json:"ts"`
}

func NewDB() (*DB, error) {
	client, err := opensearchapi.NewClient(
		opensearchapi.Config{
			Client: opensearch.Config{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
				Addresses: []string{storageHost},
				Username:  storageUsername,
				Password:  storagePassword,
			},
		},
	)
	if err != nil {
		return nil, err
	}

	db := DB{ctx: context.Background(), client: client}
	return &db, nil
}

func (db *DB) printVersion() {
	infoResp, err := db.client.Info(db.ctx, nil)
	if err != nil {
		fmt.Println("Ошибка получения информации о вресии OpenSearch")
	}
	fmt.Printf("Cluster INFO:\n  Cluster Name: %s\n  Cluster UUID: %s\n  Version Number: %s\n", infoResp.ClusterName, infoResp.ClusterUUID, infoResp.Version.Number)
}

func (db *DB) createIndex(indexName string, mapping *strings.Reader) {
	var m *strings.Reader
	if mapping == nil {
		m = defaultMapping
	} else {
		m = mapping
	}
	res, err := db.client.Indices.Create(
		db.ctx,
		opensearchapi.IndicesCreateReq{
			Index: indexName,
			Body:  m,
		},
	)

	var opensearchError *opensearch.StructError

	if err != nil {
		if errors.As(err, &opensearchError) {
			if opensearchError.Err.Type != "resource_already_exists_exception" {
				fmt.Println(err)
			}
		} else {
			fmt.Println(err)
		}
	}
	fmt.Printf("Created Index: %s\n  Shards Acknowledged: %t\n", res.Index, res.ShardsAcknowledged)
}

func (db *DB) insert(docId string, doc io.Reader, index string) error {
	_, err := db.client.Index(
		db.ctx,
		opensearchapi.IndexReq{
			Index:      index,
			DocumentID: docId,
			Body:       doc,
			Params: opensearchapi.IndexParams{
				Refresh: "true",
			},
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *DB) search(query *strings.Reader, indices []string) *[]opensearchapi.SearchHit {
	searchResp, err := db.client.Search(
		db.ctx,
		&opensearchapi.SearchReq{
			Indices: indices,
			Body:    query,
		},
	)
	if err != nil {
		fmt.Println(err)
	}

	if searchResp.Hits.Total.Value > 0 {
		indices := make([]string, 0)
		hits := &searchResp.Hits.Hits
		for _, hit := range searchResp.Hits.Hits {
			add := true
			for _, index := range indices {
				if index == hit.Index {
					add = false
				}
			}
			if add {
				indices = append(indices, hit.Index)
			}
		}
		return hits
	}
	return nil
}
