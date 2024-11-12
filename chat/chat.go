package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	"github.com/opensearch-project/opensearch-go/v4/opensearchutil"
	"golang.org/x/net/websocket"
)

var tags = TagList{Tags: []string{}}

type Server struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]connUpdTime
	db    *DB
}

type Message struct {
	ID      string `json:"id"`
	Tag     string `json:"tag"`
	Content string `json:"content"`
	TS      int64  `json:"ts"`
}

type TagList struct {
	mu   sync.Mutex
	Tags []string `json:"tags"`
}

type connUpdTime struct {
	lastMessageUpd int64
	lastTagUpd     int64
}

func (ut *connUpdTime) needsUpdate() bool {
	if ut.lastMessageUpd > ut.lastTagUpd {
		return true
	} else {
		return false
	}
}

func (ut *connUpdTime) inUpdateRange(ts int64) bool {
	if ut.lastTagUpd < ts && ut.lastMessageUpd >= ts {
		return true
	} else {
		return false
	}
}

func NewServer() *Server {
	return &Server{
		conns: make(map[*websocket.Conn]connUpdTime),
	}
}

func (s *Server) handleWS(ws *websocket.Conn) {
	fmt.Println("входящее соединение от:", ws.RemoteAddr())

	s.addConn(ws)
	s.sendInitialMsgs(ws)
	s.readLoop(ws)
}

func (s *Server) addConn(ws *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	ut := connUpdTime{
		lastMessageUpd: now,
		lastTagUpd:     now,
	}
	s.conns[ws] = ut
}

func (s *Server) sendInitialMsgs(ws *websocket.Conn) {
	query := strings.NewReader(`{"size": 10, "sort": [{"ts": {"order": "desc"}}]}`)
	if hits := s.db.search(query, []string{"chat"}); hits != nil {
		msgs := make([]Message, 0, 10)
		for _, hit := range *hits {
			var doc Document
			err := json.Unmarshal(hit.Source, &doc)
			if err != nil {
				fmt.Println("Error unmarshaling:", err)
			}
			msg := Message{
				ID:      hit.ID,
				Tag:     doc.Tag,
				Content: doc.Content,
				TS:      doc.TS,
			}
			msgs = append(msgs, msg)
		}

		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		for _, msg := range msgs {
			if err := websocket.JSON.Send(ws, msg); err != nil {
				fmt.Println("ошибка отправки ws:", err)
			}
		}
	}

}

func (s *Server) readLoop(ws *websocket.Conn) {
	for {
		var msg Message
		err := websocket.JSON.Receive(ws, &msg)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("ошибка чтения:", err)
			continue
		}
		msg.ID = uuid.New().String()
		msg.TS = time.Now().Unix()
		doci := Document{
			Tag:     msg.Tag,
			Content: msg.Content,
			TS:      msg.TS,
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			docReader := opensearchutil.NewJSONReader(doci)
			if err := s.db.insert(msg.ID, docReader, "chat"); err != nil {
				fmt.Println("Ошибка сохранения:", err)
			}
		}()
		s.broadcast(msg)
		wg.Wait()
	}
}

func (s *Server) getTagsByRange(ts int64) []Message {
	buildQuery := fmt.Sprintf(`{
        "query": {
            "bool": {
                "must": [{"range": {"ts": {"lte": %d}}}],
                "must_not": [{"term": {"tag.keyword": ""}}]
            }
        },
        "size": 10,
        "sort": [{"ts": {"order": "desc"}}]
    }`, ts)
	query := strings.NewReader(buildQuery)
	if hits := s.db.search(query, []string{"chat"}); hits != nil {
		msgs := make([]Message, 0, 10)
		for _, hit := range *hits {
			var doc Document
			err := json.Unmarshal(hit.Source, &doc)
			if err != nil {
				fmt.Println("Error unmarshaling:", err)
			}
			msg := Message{
				ID:      hit.ID,
				Tag:     doc.Tag,
				Content: "",
				TS:      doc.TS,
			}
			msgs = append(msgs, msg)
		}
		return msgs
	}
	return nil
}

func (s *Server) startTagSender() {
	throttle := 30
	throttleEnv, err := strconv.Atoi(os.Getenv("THROTTLE"))
	if err == nil {
		throttle = throttleEnv
	}
	for {
		fmt.Println("Отправка тэгов")

		if msgs := s.getTagsByRange(time.Now().Unix()); msgs != nil {
			lastUpd := msgs[0].TS

			s.mu.Lock()
			for ws, ut := range s.conns {
				if ut.needsUpdate() {
					for _, msg := range msgs {
						if ut.inUpdateRange(msg.TS) {
							if err := websocket.JSON.Send(ws, msg); err != nil {
								fmt.Println("ошибка отправки ws:", err)
							}
							fmt.Println("Тэг отправлен")
						}
					}

					ut.lastTagUpd = lastUpd
					if _, ok := s.conns[ws]; ok {
						s.conns[ws] = ut
					}
				}
			}
			s.mu.Unlock()
		}
		time.Sleep(time.Second * time.Duration(throttle))
	}
}

func (s *Server) broadcast(msg Message) {
	s.mu.Lock()
	var wg sync.WaitGroup
	for ws := range s.conns {
		wg.Add(1)
		go func(ws *websocket.Conn) {
			defer wg.Done()
			if err := websocket.JSON.Send(ws, msg); err != nil {
				s.mu.Lock()
				delete(s.conns, ws)
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				ut := s.conns[ws]
				ut.lastMessageUpd = msg.TS
				s.conns[ws] = ut
				s.mu.Unlock()
			}
		}(ws)
	}

	s.mu.Unlock()
	wg.Wait()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func handleTags(w http.ResponseWriter, r *http.Request) {
	sendTags(&w)
}

func (db *DB) addInitialTags() {
	docReader := strings.NewReader(`{"tags": ["Природа", "Космос", "Животные", "Технологии", "Интернет"]}`)
	if err := db.insert("1", docReader, "tags"); err != nil {
		fmt.Println("Ошибка сохранения:", err)
		panic("Остановка приложения")
	}
}

func (db *DB) getInitialTags() {
	resp, err := db.client.Document.Get(db.ctx, opensearchapi.DocumentGetReq{Index: "tags", DocumentID: "1"})
	if err != nil {
		fmt.Println("Ошибка получения лейблов для тэгов", err)
	}

	var oldTagList TagList
	err = json.Unmarshal(resp.Source, &oldTagList)
	if err != nil {
		fmt.Println("Error unmarshaling:", err)
		db.addInitialTags()
		db.getInitialTags()
	} else {
		tags.mu.Lock()
		tags.Tags = oldTagList.Tags
		tags.mu.Unlock()
	}
}

func contains(slice []string, str string) bool {
	for _, item := range slice {
		if strings.EqualFold(str, item) {
			return true
		}
	}
	return false
}

func sendTags(w *http.ResponseWriter) {
	tags.mu.Lock()
	for _, tag := range tags.Tags {
		fmt.Fprintf(*w, `<li>%s
		<button class='btn-del' 
		hx-delete='/tags/%s' 
		hx-target='closest li' 
		hx-swap='outerHTML' 
		hx-confirm='Вы действительно хотите удалить тэг?'>
		Удалить
		</button>
		</li>`, tag, tag)
	}
	tags.mu.Unlock()
}

func (s *Server) handleAddTag() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		newtag := r.FormValue("tagInput")

		tags.mu.Lock()
		pendingList := make([]string, len(tags.Tags), len(tags.Tags)+1)
		copy(pendingList, tags.Tags)
		tags.mu.Unlock()
		if !contains(pendingList, newtag) {
			pendingList = append(pendingList, newtag)

			tagsString := strings.Join(pendingList, "\", \"")
			tagsString = `["` + tagsString + `"]`
			_, err := s.db.client.Update(s.db.ctx, opensearchapi.UpdateReq{
				Index:      "tags",
				DocumentID: "1",
				Body:       strings.NewReader(fmt.Sprintf(`{ "doc": { "tags": %s } }`, tagsString))})

			if err != nil {
				fmt.Println("Ошибка обновления лейблов для тэгов")
			} else {
				tags.mu.Lock()
				tags.Tags = pendingList
				tags.mu.Unlock()
			}
		}
		sendTags(&w)
	}
}

func removeElement(slice []string, value string) []string {
	for i, v := range slice {
		if v == value {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func (s *Server) handleDeleteTag() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		delTag := parts[2]

		tags.mu.Lock()
		pendingList := make([]string, len(tags.Tags))
		copy(pendingList, tags.Tags)
		tags.mu.Unlock()

		pendingList = removeElement(pendingList, delTag)
		tagsString := strings.Join(pendingList, "\", \"")
		tagsString = `["` + tagsString + `"]`
		_, err := s.db.client.Update(s.db.ctx, opensearchapi.UpdateReq{
			Index:      "tags",
			DocumentID: "1",
			Body:       strings.NewReader(fmt.Sprintf(`{ "doc": { "tags": %s } }`, tagsString))})

		if err != nil {
			fmt.Println("Ошибка обновления лейблов для тэгов")
			w.WriteHeader(http.StatusNoContent)
		} else {
			tags.mu.Lock()
			tags.Tags = pendingList
			tags.mu.Unlock()

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "")
		}
	}
}

func (s *Server) handleSearch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		q := params.Get("q")
		query := strings.NewReader(fmt.Sprintf(`{
		"query": {"multi_match": {"query": "%s", "fields": ["tag^2", "content"]}},
		"size": 10}`, q))
		if hits := s.db.search(query, []string{"chat"}); hits != nil {
			for _, hit := range *hits {
				var doc Document
				err := json.Unmarshal(hit.Source, &doc)
				if err != nil {
					fmt.Println("Error unmarshaling:", err)
				}
				fmt.Fprintf(w, `<div class="message">
				<div class="messageTag">%s</div>
				<div class="messageText">%s</div>
				</div>`, doc.Tag, doc.Content)
			}
		}
	}
}

func main() {
	db, err := NewDB()
	if err != nil {
		fmt.Println("Ошибка подключения к OpenSearch", err)
		panic("Остановка приложения")
	}
	db.printVersion()
	chatMapping := strings.NewReader(`{
		"mappings": {
			"properties": {
				"content": {
					"type": "text",
					"fields": {"keyword": {"type": "keyword", "ignore_above": 256}}
				},
				"tag": {
					"type": "text",
					"fields": {"keyword": {"type": "keyword", "ignore_above": 256}}
				},
				"ts": {"type": "long"}
			}
		}
	}`)
	db.createIndex("chat", chatMapping)
	tagsMapping := strings.NewReader(`{"mappings": {"properties": {"tags": {"type": "keyword"}}}}`)
	db.createIndex("tags", tagsMapping)
	db.getInitialTags()
	server := NewServer()
	server.db = db
	go server.startTagSender()
	http.Handle("GET /ws", websocket.Handler(server.handleWS))
	http.HandleFunc("GET /", handleIndex)
	http.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("GET /tags", handleTags)
	http.HandleFunc("POST /add-tag", server.handleAddTag())
	http.HandleFunc("DELETE /tags/", server.handleDeleteTag())
	http.HandleFunc("GET /search", server.handleSearch())
	fmt.Println("Сервер запущен на :8001")
	http.ListenAndServe(":8001", nil)
}
