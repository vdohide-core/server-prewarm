package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"server-prewarm/internal/config"
	"server-prewarm/internal/db/models"
	"server-prewarm/internal/manager"
	"server-prewarm/internal/scanner"
	"server-prewarm/internal/ws"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RegisterRoutes sets up all HTTP routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", HandleStatus)
	mux.HandleFunc("/api/logs", HandleLogs)
	mux.HandleFunc("/api/start", HandleStart)
	mux.HandleFunc("/ws", ws.GetHub().HandleWS)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(GetStaticFS())))
	mux.HandleFunc("/", HandleDashboard)
}

func HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manager.GetManager().GetStatus())
}

// mediaDoc for decoding MongoDB media with per-POP prewarm
type mediaDoc struct {
	ID         string                         `bson:"_id"`
	Slug       string                         `bson:"slug"`
	FileID     *string                        `bson:"fileId"`
	Resolution *string                        `bson:"resolution"`
	Prewarm    map[string]models.PrewarmEntry `bson:"prewarm"`
}

type LogEntry struct {
	Slug       string `json:"slug"`
	FileSlug   string `json:"fileSlug"`
	Resolution string `json:"resolution"`
	Total      int64  `json:"total"`
	Hit        int64  `json:"hit"`
	Miss       int64  `json:"miss"`
	Expired    int64  `json:"expired"`
	Failed     int64  `json:"failed"`
	HitRate    string `json:"hitRate"`
	PrewarmAt  string `json:"prewarmAt"`
}

type LogsResponse struct {
	Logs       []LogEntry `json:"logs"`
	Page       int        `json:"page"`
	PerPage    int        `json:"perPage"`
	Total      int        `json:"total"`
	TotalPages int        `json:"totalPages"`
}

func HandleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 10
	searchQ := r.URL.Query().Get("q")

	ctx := r.Context()
	storageID := config.AppConfig.StorageID

	// Filter: media with prewarm for this POP
	pop := config.AppConfig.PrewarmPOP
	filter := bson.M{
		"type":       models.MediaTypeVideo,
		"resolution": bson.M{"$in": scanner.VideoResolutions},
		"deletedAt":  nil,
		"prewarm." + pop: bson.M{"$exists": true},
	}
	if storageID != "" {
		filter["storageId"] = storageID
	}

	// Search: find matching postIds first (Files in vdohide-core)
	if searchQ != "" {
		postCursor, err := models.FileModel.Col().Find(ctx, bson.M{
			"slug": bson.M{"$regex": searchQ, "$options": "i"},
			"type": models.FileTypeVideo,
		})
		if err == nil {
			defer postCursor.Close(ctx)
			var matchIDs []string
			for postCursor.Next(ctx) {
				var p models.File
				if err := postCursor.Decode(&p); err == nil {
					matchIDs = append(matchIDs, p.ID)
				}
			}
			if len(matchIDs) > 0 {
				filter["fileId"] = bson.M{"$in": matchIDs}
			} else {
				// No post matches, return empty
				json.NewEncoder(w).Encode(LogsResponse{Logs: []LogEntry{}, Page: 1, PerPage: perPage, Total: 0, TotalPages: 1})
				return
			}
		}
	}

	totalCount, _ := models.MediaModel.Col().CountDocuments(ctx, filter)
	total := int(totalCount)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// Query with pagination + sorting
	skip := int64((page - 1) * perPage)

	// Map sort keys to MongoDB fields
	sortKey := r.URL.Query().Get("sort")
	sortDir := -1
	if r.URL.Query().Get("dir") == "1" {
		sortDir = 1
	}

	sortFieldMap := map[string]string{
		"fileSlug":   "fileId",
		"resolution": "resolution",
		"total":      "prewarm." + pop + ".data.total",
		"hit":        "prewarm." + pop + ".data.hit",
		"miss":       "prewarm." + pop + ".data.miss",
		"hitRate":    "prewarm." + pop + ".data.hit",
		"prewarmAt":  "prewarm." + pop + ".prewarmAt",
	}
	sortField := "prewarm." + pop + ".prewarmAt"
	if f, ok := sortFieldMap[sortKey]; ok {
		sortField = f
	}

	opts := options.Find().
		SetSort(bson.D{{Key: sortField, Value: sortDir}}).
		SetSkip(skip).
		SetLimit(int64(perPage))

	cursor, err := models.MediaModel.Col().Find(ctx, filter, opts)
	if err != nil {
		json.NewEncoder(w).Encode(LogsResponse{Logs: []LogEntry{}, Page: 1, PerPage: perPage, Total: 0, TotalPages: 1})
		return
	}
	defer cursor.Close(ctx)

	var medias []mediaDoc
	if err := cursor.All(ctx, &medias); err != nil {
		json.NewEncoder(w).Encode(LogsResponse{Logs: []LogEntry{}, Page: 1, PerPage: perPage, Total: 0, TotalPages: 1})
		return
	}

	// Batch fetch post slugs (File slugs in vdohide-core)
	postIdSet := make(map[string]bool)
	for _, m := range medias {
		if m.FileID != nil {
			postIdSet[*m.FileID] = true
		}
	}
	postIds := make([]string, 0, len(postIdSet))
	for id := range postIdSet {
		postIds = append(postIds, id)
	}

	postMap := make(map[string]string) // fileId -> slug
	if len(postIds) > 0 {
		postCursor, err := models.FileModel.Col().Find(ctx, bson.M{"_id": bson.M{"$in": postIds}})
		if err == nil {
			defer postCursor.Close(ctx)
			for postCursor.Next(ctx) {
				var p models.File
				if err := postCursor.Decode(&p); err == nil {
					postMap[p.ID] = p.Slug
				}
			}
		}
	}

	// Build response
	logs := make([]LogEntry, 0, len(medias))
	for _, m := range medias {
		fileSlug := ""
		if m.FileID != nil {
			fileSlug = postMap[*m.FileID]
		}
		if fileSlug == "" && m.FileID != nil {
			fileSlug = *m.FileID
		}

		popData, ok := m.Prewarm[pop]
		var total, hit, miss, expired, failed int
		var prewarmAt time.Time
		if ok && popData.Data != nil {
			total = popData.Data.Total
			hit = popData.Data.Hit
			miss = popData.Data.Miss
			expired = popData.Data.Expired
			failed = popData.Data.Failed
			if popData.PrewarmAt != nil {
				prewarmAt = *popData.PrewarmAt
			}
		}

		hitRate := "0%"
		t := hit + miss + failed
		if t > 0 {
			hitRate = fmt.Sprintf("%.0f%%", float64(hit)/float64(t)*100)
		}

		resVal := ""
		if m.Resolution != nil {
			resVal = *m.Resolution
		}

		logs = append(logs, LogEntry{
			Slug:       m.Slug,
			FileSlug:   fileSlug,
			Resolution: resVal,
			Total:      int64(total),
			Hit:        int64(hit),
			Miss:       int64(miss),
			Expired:    int64(expired),
			Failed:     int64(failed),
			HitRate:    hitRate,
			PrewarmAt:  prewarmAt.Format(time.RFC3339),
		})
	}

	json.NewEncoder(w).Encode(LogsResponse{
		Logs:       logs,
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
	})
}

func HandleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	manager.GetManager().StartPrewarm(context.Background())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	f, err := templateFS.Open("templates/index.html")
	if err != nil {
		http.Error(w, "Dashboard not found", 500)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.Copy(w, f)
}
