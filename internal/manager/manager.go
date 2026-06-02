package manager

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server-prewarm/internal/config"
	"server-prewarm/internal/db/models"
	"server-prewarm/internal/scanner"
	"server-prewarm/internal/worker"
	"server-prewarm/internal/ws"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/google/uuid"
)

// JobStatus represents the state of a prewarm job
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// Job represents a single prewarm target being processed
type Job struct {
	ID          string        `json:"id"`
	FileSlug    string        `json:"fileSlug"`
	FileTitle   string        `json:"fileTitle"`
	MediaID     string        `json:"mediaId"`
	MediaSlug   string        `json:"mediaSlug"`
	Resolution  string        `json:"resolution"`
	PlaylistURL string        `json:"playlistUrl"`
	Status      JobStatus     `json:"status"`
	Total       int64         `json:"total"`
	Progress    int64         `json:"progress"`
	Hit         int64         `json:"hit"`
	Miss        int64         `json:"miss"`
	Expired     int64         `json:"expired"`
	Failed      int64         `json:"failed"`
	StartedAt   *time.Time    `json:"startedAt,omitempty"`
	CompletedAt *time.Time    `json:"completedAt,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
}

// OverallStatus represents the global prewarm status
type OverallStatus struct {
	State         string `json:"state"` // idle, running, completed
	StorageName   string `json:"storageName,omitempty"`
	POP           string `json:"pop,omitempty"`
	TotalMedia    int64  `json:"totalMedia"`
	Pending       int64  `json:"pending"` // media without prewarm for this POP
	Processed     int64  `json:"processed"`
	TotalHit      int64  `json:"totalHit"`
	TotalMiss     int64  `json:"totalMiss"`
	TotalFailed   int64  `json:"totalFailed"`
	Elapsed       string `json:"elapsed,omitempty"`
	DomainContent string `json:"domainContent,omitempty"`
	RefDomain     string `json:"refDomain,omitempty"`
}

// Manager manages the continuous prewarm process
type Manager struct {
	mu          sync.RWMutex
	state       string
	scanCfg     *scanner.ScanConfig
	recentJobs  []*Job // last N completed jobs for dashboard
	processed   int64
	totalHit    int64
	totalMiss   int64
	totalFailed int64
	startedAt   time.Time
	pending     int64
	totalMedia  int64
	inFlight    map[string]string // mediaID -> jobType ("new" or "reprewarm")
}

var instance *Manager

func GetManager() *Manager {
	if instance == nil {
		instance = &Manager{
			state:    "idle",
			inFlight: make(map[string]string),
		}
	}
	return instance
}

// GetStatus returns the current overall status
func (m *Manager) GetStatus() OverallStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := OverallStatus{
		State:       m.state,
		POP:         config.AppConfig.PrewarmPOP,
		TotalMedia:  m.totalMedia,
		Pending:     m.pending,
		Processed:   m.processed,
		TotalHit:    m.totalHit,
		TotalMiss:   m.totalMiss,
		TotalFailed: m.totalFailed,
	}

	if m.scanCfg != nil {
		s.DomainContent = m.scanCfg.DomainContent
		s.RefDomain = m.scanCfg.RefDomain
	}

	// Get storage name
	storageID := config.AppConfig.StorageID
	if storageID != "" {
		var storage models.Storage
		err := models.StorageModel.Col().FindOne(context.Background(), bson.M{"_id": storageID}).Decode(&storage)
		if err == nil {
			s.StorageName = storage.Name
		}
	}

	if m.state == "running" {
		s.Elapsed = time.Since(m.startedAt).Round(time.Second).String()
	}

	return s
}

// GetRecentJobs returns the recent completed jobs
func (m *Manager) GetRecentJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recentJobs
}

// releaseAllInFlightLocks unsets processing status locks for all currently running jobs on shutdown/abort
func (m *Manager) releaseAllInFlightLocks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	pop := config.AppConfig.PrewarmPOP
	if len(m.inFlight) == 0 {
		return
	}

	log.Printf("🧹 Releasing %d in-flight locks...", len(m.inFlight))
	for mediaID := range m.inFlight {
		update := bson.M{
			"$unset": bson.M{
				"prewarm." + pop + ".status":   "",
				"prewarm." + pop + ".lockedAt": "",
			},
		}
		_, err := models.MediaModel.Col().UpdateOne(context.Background(), bson.M{"_id": mediaID}, update)
		if err != nil {
			log.Printf("⚠️ Failed to release lock for %s: %v", mediaID, err)
		}
	}
	m.inFlight = make(map[string]string)
}

// StartPrewarm launches the continuous prewarm process
func (m *Manager) StartPrewarm(ctx context.Context) {
	m.mu.Lock()
	if m.state == "running" {
		m.mu.Unlock()
		log.Println("⚠️ Prewarm already running")
		return
	}
	m.state = "running"
	m.startedAt = time.Now()
	m.recentJobs = nil
	m.mu.Unlock()

	go m.runContinuous(ctx)
}

func (m *Manager) runContinuous(ctx context.Context) {
	defer m.releaseAllInFlightLocks()
	log.Println("🔍 Loading settings...")

	scanCfg, err := scanner.LoadScanConfig(ctx)
	if err != nil {
		log.Printf("❌ Failed to load config: %v", err)
		m.mu.Lock()
		m.state = "idle"
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	m.scanCfg = scanCfg
	m.mu.Unlock()

	// Auto-detect POP from storage publicUrls if needed
	if config.NeedAutoDetectPOP() {
		storageCursor, sErr := models.StorageModel.Col().Find(ctx, bson.M{
			"enable":  true,
			"status":  "online",
			"accepts": "video",
		})
		var detectURLs []string
		if sErr == nil {
			defer storageCursor.Close(ctx)
			for storageCursor.Next(ctx) {
				var s models.Storage
				if err := storageCursor.Decode(&s); err == nil && s.PublicURL != nil && *s.PublicURL != "" {
					u := *s.PublicURL
					if !strings.HasPrefix(u, "http") {
						u = "https://" + u
					}
					detectURLs = append(detectURLs, u)
				}
			}
		}

		if len(detectURLs) == 0 {
			log.Printf("⚠️ No active video storages found, falling back to 'fra'")
			config.SetPOP("fra")
		} else {
			pop, err := config.DetectPOP(detectURLs)
			if err != nil {
				log.Printf("⚠️ Failed to detect POP: %v, falling back to 'fra'", err)
				config.SetPOP("fra")
			} else if pop == "" {
				log.Printf("⚠️ No CF-Ray header found, falling back to 'fra'")
				config.SetPOP("fra")
			} else {
				config.SetPOP(pop)
			}
		}
	}

	// Count pending + total
	pending, total := scanner.CountPending(ctx)

	// Load historical stats from DB (survive restarts)
	processed, totalHit, totalMiss, totalFailed := loadStatsFromDB(ctx)

	m.mu.Lock()
	m.pending = pending
	m.totalMedia = total
	m.processed = processed
	m.totalHit = totalHit
	m.totalMiss = totalMiss
	m.totalFailed = totalFailed
	m.mu.Unlock()

	log.Printf("📊 Total media: %d | Pending: %d | Already processed: %d (H:%d M:%d F:%d)",
		total, pending, processed, totalHit, totalMiss, totalFailed)

	initialCfg := GetPrewarmSetting(ctx)
	log.Printf("⚙️ Settings loaded: enabled=%t, enabled_old=%t, reprewarm_age_minutes=%d, concurrent_new=%d, concurrent_old=%d",
		initialCfg.Enabled, initialCfg.EnabledOld, initialCfg.ReprewarmAgeMinutes, initialCfg.PrewarmMaxConcurrent, initialCfg.PrewarmOldMaxConcurrent)

	// Track active job counts by type
	var activeNew int64
	var activeReprewarm int64

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️ Prewarm stopped (context cancelled)")
			m.mu.Lock()
			m.state = "idle"
			m.mu.Unlock()
			return
		default:
		}

		// Read dynamic settings from MongoDB "prewarm" key
		prewarmCfg := GetPrewarmSetting(ctx)

		// Check prewarm.enabled setting
		if !prewarmCfg.Enabled {
			log.Println("⏸️ prewarm.enabled = false, waiting 30s...")
			m.mu.Lock()
			m.state = "paused"
			m.mu.Unlock()
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		// Check storage.enable_prewarm
		if !isStoragePrewarmEnabled(ctx) {
			log.Println("⏸️ storage enable_prewarm = false, waiting 30s...")
			m.mu.Lock()
			m.state = "paused"
			m.mu.Unlock()
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		m.mu.Lock()
		m.state = "running"
		m.mu.Unlock()

		// Concurrency settings from dynamic config
		concNew := prewarmCfg.PrewarmMaxConcurrent
		parNew := prewarmCfg.PrewarmParallel
		concOld := prewarmCfg.PrewarmOldMaxConcurrent
		parOld := prewarmCfg.PrewarmOldParallel

		if !prewarmCfg.EnabledOld {
			concOld = 0
		}

		// Re-read domain settings from DB each loop
		if newScanCfg, err := scanner.LoadScanConfig(ctx); err == nil {
			scanCfg = newScanCfg
			m.mu.Lock()
			m.scanCfg = scanCfg
			m.mu.Unlock()
		}

		// Non-FRA POPs only use prewarm_old settings
		if config.AppConfig.PrewarmPOP != "fra" {
			concNew = 0
		}

		curNew := atomic.LoadInt64(&activeNew)
		curOld := atomic.LoadInt64(&activeReprewarm)

		// Build exclude list
		m.mu.RLock()
		excludeIDs := make([]string, 0, len(m.inFlight))
		for id := range m.inFlight {
			excludeIDs = append(excludeIDs, id)
		}
		m.mu.RUnlock()

		var target *scanner.PrewarmTarget
		var jobType string
		var jobPar int

		// Try to fill a "new" slot
		if concNew > 0 && curNew < int64(concNew) {
			t, err := scanner.FetchNew(ctx, scanCfg, excludeIDs)
			if err != nil {
				log.Printf("❌ FetchNew error: %v", err)
			} else if t != nil {
				target = t
				jobType = "new"
				jobPar = parNew
			}
		}

		// Try to fill a "reprewarm" slot (independent from new)
		if target == nil && concOld > 0 && curOld < int64(concOld) {
			reprewarmAge := time.Duration(prewarmCfg.ReprewarmAgeMinutes) * time.Minute
			t, err := scanner.FetchReprewarm(ctx, scanCfg, reprewarmAge, excludeIDs)
			if err != nil {
				log.Printf("❌ FetchReprewarm error: %v", err)
			} else if t != nil {
				target = t
				jobType = "reprewarm"
				jobPar = parOld
			}
		}

		if target == nil {
			// Both types found nothing — wait 5s
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		t := *target

		// Track as in-flight
		m.mu.Lock()
		m.inFlight[t.MediaID] = jobType
		m.mu.Unlock()
		if jobType == "new" {
			atomic.AddInt64(&activeNew, 1)
		} else {
			atomic.AddInt64(&activeReprewarm, 1)
		}

		go func(t scanner.PrewarmTarget, parallel int, jType string) {
			defer func() {
				if jType == "new" {
					atomic.AddInt64(&activeNew, -1)
				} else {
					atomic.AddInt64(&activeReprewarm, -1)
				}
				m.mu.Lock()
				delete(m.inFlight, t.MediaID)
				m.mu.Unlock()
			}()

			job := &Job{
				ID:          t.MediaID,
				FileSlug:    t.FileSlug,
				FileTitle:   t.FileTitle,
				MediaID:     t.MediaID,
				MediaSlug:   t.MediaSlug,
				Resolution:  t.Resolution,
				PlaylistURL: t.PlaylistURL,
				Status:      JobRunning,
			}

			now := time.Now()
			job.StartedAt = &now

			log.Printf("🔥 [%s] %s [%s]", t.MediaSlug, t.FileSlug, t.Resolution)

			// Broadcast job start
			ws.GetHub().Broadcast("job_start", map[string]string{
				"mediaSlug":  t.MediaSlug,
				"fileSlug":   t.FileSlug,
				"resolution": t.Resolution,
			})

			// Run prewarm
			statsCh := make(chan worker.Stats, 1)
			worker.PrewarmPlaylist(t, worker.PrewarmOptions{
				Parallel:  parallel,
				RefDomain: scanCfg.RefDomain,
			}, statsCh)

			stats := <-statsCh

			completed := time.Now()
			job.Status = JobCompleted
			job.CompletedAt = &completed
			job.Duration = completed.Sub(*job.StartedAt)
			job.Total = stats.Total
			job.Hit = stats.Hit
			job.Miss = stats.Miss
			job.Expired = stats.Expired
			job.Failed = stats.Failed
			job.Progress = stats.Progress

			// Update MongoDB media.prewarm
			prewarmData := models.PrewarmEntry{
				Data: &models.PrewarmData{
					Total:   int(stats.Total),
					Hit:     int(stats.Hit),
					Miss:    int(stats.Miss),
					Expired: int(stats.Expired),
					Failed:  int(stats.Failed),
				},
				PrewarmAt: &completed,
			}

			_, updateErr := models.MediaModel.Col().UpdateOne(ctx, bson.M{"_id": t.MediaID}, bson.M{
				"$set": bson.M{"prewarm." + config.AppConfig.PrewarmPOP: prewarmData},
			})
			if updateErr != nil {
				log.Printf("⚠️ Failed to update media %s: %v", t.MediaSlug, updateErr)
			}

			m.mu.Lock()
			m.recentJobs = append(m.recentJobs, job)
			if len(m.recentJobs) > 100 {
				m.recentJobs = m.recentJobs[len(m.recentJobs)-100:]
			}
			m.mu.Unlock()

			log.Printf("✅ [%s] Done: %d URLs (HIT:%d MISS:%d) in %v",
				t.MediaSlug, stats.Total, stats.Hit, stats.Miss,
				job.Duration.Round(time.Second))

			// Broadcast job complete
			ws.GetHub().Broadcast("job_complete", map[string]interface{}{
				"mediaSlug":  t.MediaSlug,
				"fileSlug":   t.FileSlug,
				"resolution": t.Resolution,
				"total":      stats.Total,
				"hit":        stats.Hit,
				"miss":       stats.Miss,
			})

			// Re-read all stats from DB (accurate, no double-counting)
			pending, total := scanner.CountPending(ctx)
			processed, totalHit, totalMiss, totalFailed := loadStatsFromDB(ctx)
			m.mu.Lock()
			m.pending = pending
			m.totalMedia = total
			m.processed = processed
			m.totalHit = totalHit
			m.totalMiss = totalMiss
			m.totalFailed = totalFailed
			m.mu.Unlock()
		}(t, jobPar, jobType)
	}
}

type PrewarmSetting struct {
	Enabled                 bool `bson:"enabled"`
	EnabledOld              bool `bson:"enabled_old"`
	ReprewarmAgeMinutes     int  `bson:"reprewarm_age_minutes"`
	PrewarmMaxConcurrent    int  `bson:"prewarm_max_concurrent"`
	PrewarmOldMaxConcurrent int  `bson:"prewarm_old_max_concurrent"`
	PrewarmParallel         int  `bson:"prewarm_parallel"`
	PrewarmOldParallel      int  `bson:"prewarm_old_parallel"`
}

// GetPrewarmSetting retrieves the prewarm settings object from MongoDB or returns/saves defaults.
func GetPrewarmSetting(ctx context.Context) PrewarmSetting {
	defaultSetting := PrewarmSetting{
		Enabled:                 true,
		EnabledOld:              true,
		ReprewarmAgeMinutes:     10,
		PrewarmMaxConcurrent:    1,
		PrewarmOldMaxConcurrent: 5,
		PrewarmParallel:         10,
		PrewarmOldParallel:      20,
	}

	var setting struct {
		Value bson.M `bson:"value"`
	}
	err := models.SettingModel.Col().FindOne(ctx, bson.M{"name": "prewarm"}).Decode(&setting)
	if err != nil {
		// Not found! Let's insert the default setting so it can be edited via DB
		go func() {
			dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = models.SettingModel.Col().InsertOne(dbCtx, bson.M{
				"_id":  uuid.New().String(),
				"name": "prewarm",
				"value": bson.M{
					"enabled":                    defaultSetting.Enabled,
					"enabled_old":                defaultSetting.EnabledOld,
					"reprewarm_age_minutes":      defaultSetting.ReprewarmAgeMinutes,
					"prewarm_max_concurrent":     defaultSetting.PrewarmMaxConcurrent,
					"prewarm_old_max_concurrent": defaultSetting.PrewarmOldMaxConcurrent,
					"prewarm_parallel":           defaultSetting.PrewarmParallel,
					"prewarm_old_parallel":       defaultSetting.PrewarmOldParallel,
				},
				"createdAt": time.Now(),
				"updatedAt": time.Now(),
			})
		}()
		return defaultSetting
	}

	val := setting.Value
	if val == nil {
		return defaultSetting
	}

	result := defaultSetting
	if v, exists := val["enabled"]; exists {
		if b, ok := v.(bool); ok {
			result.Enabled = b
		}
	}
	if v, exists := val["enabled_old"]; exists {
		if b, ok := v.(bool); ok {
			result.EnabledOld = b
		}
	}
	if v, exists := val["reprewarm_age_minutes"]; exists {
		result.ReprewarmAgeMinutes = toInt(v)
	}
	if v, exists := val["prewarm_max_concurrent"]; exists {
		result.PrewarmMaxConcurrent = toInt(v)
	}
	if v, exists := val["prewarm_old_max_concurrent"]; exists {
		result.PrewarmOldMaxConcurrent = toInt(v)
	}
	if v, exists := val["prewarm_parallel"]; exists {
		result.PrewarmParallel = toInt(v)
	}
	if v, exists := val["prewarm_old_parallel"]; exists {
		result.PrewarmOldParallel = toInt(v)
	}

	return result
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return 0
}

// isStoragePrewarmEnabled checks enable_prewarm field on the storage document
func isStoragePrewarmEnabled(ctx context.Context) bool {
	storageID := config.AppConfig.StorageID
	if storageID == "" {
		return true // no storage filter, allow
	}

	var storage struct {
		Enable        bool `bson:"enable"`
		EnablePrewarm bool `bson:"enable_prewarm"`
	}
	err := models.StorageModel.Col().FindOne(ctx, bson.M{"_id": storageID}).Decode(&storage)
	if err != nil {
		return true // default to true if not found
	}

	if !storage.Enable {
		return false
	}

	var raw bson.M
	_ = models.StorageModel.Col().FindOne(ctx, bson.M{"_id": storageID}).Decode(&raw)
	if raw != nil {
		if val, exists := raw["enable_prewarm"]; exists {
			if b, ok := val.(bool); ok {
				return b
			}
		}
	}

	return true
}

// loadStatsFromDB loads historical prewarm stats from MongoDB using aggregation
func loadStatsFromDB(ctx context.Context) (processed int64, totalHit int64, totalMiss int64, totalFailed int64) {
	storageID := config.AppConfig.StorageID
	pop := config.AppConfig.PrewarmPOP

	matchFilter := bson.M{
		"deletedAt":      nil,
		"prewarm." + pop: bson.M{"$exists": true},
		"$or": bson.A{
			bson.M{
				"type":       models.MediaTypeVideo,
				"resolution": bson.M{"$in": scanner.VideoResolutions},
			},
			bson.M{
				"type":     models.MediaTypeThumbnail,
				"fileName": "sprite.vtt",
			},
		},
	}
	if storageID != "" {
		matchFilter["storageId"] = storageID
	}

	pipeline := bson.A{
		bson.M{"$match": matchFilter},
		bson.M{"$group": bson.M{
			"_id":         nil,
			"count":       bson.M{"$sum": 1},
			"totalHit":    bson.M{"$sum": "$prewarm." + pop + ".data.hit"},
			"totalMiss":   bson.M{"$sum": "$prewarm." + pop + ".data.miss"},
			"totalFailed": bson.M{"$sum": "$prewarm." + pop + ".data.failed"},
		}},
	}

	cursor, err := models.MediaModel.Col().Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("⚠️ Failed to aggregate prewarm stats: %v", err)
		return 0, 0, 0, 0
	}
	defer cursor.Close(ctx)

	if cursor.Next(ctx) {
		var result struct {
			Count       int64 `bson:"count"`
			TotalHit    int64 `bson:"totalHit"`
			TotalMiss   int64 `bson:"totalMiss"`
			TotalFailed int64 `bson:"totalFailed"`
		}
		if err := cursor.Decode(&result); err == nil {
			return result.Count, result.TotalHit, result.TotalMiss, result.TotalFailed
		}
	}
	return 0, 0, 0, 0
}
