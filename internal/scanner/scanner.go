package scanner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"server-prewarm/internal/config"
	"server-prewarm/internal/db/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/google/uuid"
)

// PrewarmTarget represents a single media target to prewarm
type PrewarmTarget struct {
	FileSlug    string
	FileTitle   string
	MediaID     string // MongoDB _id for updating
	MediaSlug   string
	Resolution  string
	PlaylistURL string
}

// ScanConfig holds domain info from settings
type ScanConfig struct {
	DomainContent string
	DomainStatic  string
	RefDomain     string
}

// VideoResolutions is the list of video resolutions to prewarm
var VideoResolutions = []string{models.ResolutionOriginal, models.Resolution1080, models.Resolution720, models.Resolution480, models.Resolution360}


// LoadScanConfig fetches domain_content (fallback: domain_asset), domain_static, and domain_player (fallback: domain_preview) from config/settings
func LoadScanConfig(ctx context.Context) (*ScanConfig, error) {
	domainContent := config.AppConfig.DomainContent
	if domainContent == "" {
		domainContent = getSettingValue(ctx, "domain_content")
	}
	if domainContent == "" {
		domainContent = getSettingValue(ctx, "domain_asset")
	}
	if domainContent == "" {
		return nil, fmt.Errorf("domain_content setting not found in database")
	}
	domainContent = strings.TrimRight(domainContent, "/")
	if !strings.HasPrefix(domainContent, "http") {
		domainContent = "https://" + domainContent
	}

	domainStatic := config.AppConfig.DomainStatic
	if domainStatic == "" {
		domainStatic = getSettingValue(ctx, "domain_static")
	}
	if domainStatic == "" {
		domainStatic = "static.vdohls.com"
		// Insert it into settings collection so it exists in DB
		go func() {
			dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = models.SettingModel.Col().InsertOne(dbCtx, bson.M{
				"_id":       uuid.New().String(),
				"name":      "domain_static",
				"value":     "static.vdohls.com",
				"createdAt": time.Now(),
				"updatedAt": time.Now(),
			})
		}()
	}
	domainStatic = strings.TrimRight(domainStatic, "/")
	if !strings.HasPrefix(domainStatic, "http") {
		domainStatic = "https://" + domainStatic
	}

	refDomain := config.AppConfig.DomainPlayer
	if refDomain == "" {
		refDomain = getSettingValue(ctx, "domain_player")
	}
	if refDomain == "" {
		refDomain = getSettingValue(ctx, "domain_preview")
	}
	if refDomain != "" {
		refDomain = strings.TrimRight(refDomain, "/")
		if !strings.HasPrefix(refDomain, "http") {
			refDomain = "https://" + refDomain
		}
	}

	return &ScanConfig{
		DomainContent: domainContent,
		DomainStatic:  domainStatic,
		RefDomain:     refDomain,
	}, nil
}

// releaseLock unsets status and lockedAt on the media document for this POP
func releaseLock(ctx context.Context, mediaID string, pop string) {
	update := bson.M{
		"$unset": bson.M{
			"prewarm." + pop + ".status":   "",
			"prewarm." + pop + ".lockedAt": "",
		},
	}
	_, _ = models.MediaModel.Col().UpdateOne(ctx, bson.M{"_id": mediaID}, update)
}

// findAndLockTarget atomically finds the top matching document, locks it, and verifies the parent File is ready.
// If the parent File is not ready or not found, it unlocks the media and continues searching.
func findAndLockTarget(ctx context.Context, scanCfg *ScanConfig, filter bson.M, sort bson.D, excludeIDs []string) (*PrewarmTarget, error) {
	pop := config.AppConfig.PrewarmPOP

	opts := options.FindOneAndUpdate().
		SetSort(sort).
		SetReturnDocument(options.After)

	update := bson.M{
		"$set": bson.M{
			"prewarm." + pop + ".status":   "processing",
			"prewarm." + pop + ".lockedAt": time.Now(),
		},
	}

	// Make a local copy of exclude IDs to append skip targets dynamically during loop
	localExclude := make([]string, len(excludeIDs))
	copy(localExclude, excludeIDs)

	for {
		// Clone filter to insert dynamic $nin excludeIDs
		iterFilter := make(bson.M, len(filter))
		for k, v := range filter {
			iterFilter[k] = v
		}
		if len(localExclude) > 0 {
			iterFilter["_id"] = bson.M{"$nin": localExclude}
		}

		var media models.Media
		err := models.MediaModel.Col().FindOneAndUpdate(ctx, iterFilter, update, opts).Decode(&media)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, nil // No matching targets available
			}
			return nil, fmt.Errorf("failed to FindOneAndUpdate: %w", err)
		}

		if media.FileID == nil {
			releaseLock(ctx, media.ID, pop)
			localExclude = append(localExclude, media.ID)
			continue
		}

		// Check parent File (post)
		var file models.File
		err = models.FileModel.Col().FindOne(ctx, bson.M{
			"_id":    *media.FileID,
			"status": models.FileStatusReady,
			"type":   models.FileTypeVideo,
		}).Decode(&file)
		if err != nil {
			// Parent file is not found or not ready, release lock and skip this media in future iterations
			releaseLock(ctx, media.ID, pop)
			localExclude = append(localExclude, media.ID)
			continue
		}

		// Build and return the PrewarmTarget
		var playlistURL string
		resVal := ""
		if media.Type == models.MediaTypeThumbnail {
			playlistURL = fmt.Sprintf("%s/%s/sprite/sprite.vtt", scanCfg.DomainStatic, file.Slug)
			resVal = "thumbnail"
		} else {
			playlistURL = fmt.Sprintf("%s/%s/video.m3u8", scanCfg.DomainContent, media.Slug)
			if media.Resolution != nil {
				resVal = *media.Resolution
			}
		}

		return &PrewarmTarget{
			FileSlug:    file.Slug,
			FileTitle:   file.Name,
			MediaID:     media.ID,
			MediaSlug:   media.Slug,
			Resolution:  resVal,
			PlaylistURL: playlistURL,
		}, nil
	}
}

// FetchNew fetches media that has never been prewarmed for this POP (or processing lock expired)
func FetchNew(ctx context.Context, scanCfg *ScanConfig, excludeIDs []string) (*PrewarmTarget, error) {
	storageID := config.AppConfig.StorageID
	pop := config.AppConfig.PrewarmPOP

	lockCutoff := time.Now().Add(-5 * time.Minute)

	baseCriteria := bson.M{
		"$or": bson.A{
			bson.M{
				"type":       models.MediaTypeVideo,
				"resolution": bson.M{"$in": VideoResolutions},
			},
			bson.M{
				"type":     models.MediaTypeThumbnail,
				"fileName": "sprite.vtt",
			},
		},
	}

	lockFilter := bson.M{
		"$or": bson.A{
			bson.M{"prewarm." + pop: bson.M{"$exists": false}},
			bson.M{
				"prewarm." + pop + ".status":    "processing",
				"prewarm." + pop + ".prewarmAt": bson.M{"$exists": false},
				"prewarm." + pop + ".lockedAt":  bson.M{"$lt": lockCutoff},
			},
		},
	}

	filter := bson.M{
		"deletedAt": nil,
		"$and": bson.A{
			baseCriteria,
			lockFilter,
		},
	}
	if storageID != "" {
		filter["storageId"] = storageID
	}

	return findAndLockTarget(ctx, scanCfg, filter, bson.D{{Key: "createdAt", Value: 1}}, excludeIDs)
}

// FetchReprewarm fetches media to prewarm for this POP.
// For non-FRA POPs: also matches media that FRA prewarmed but this POP hasn't yet.
func FetchReprewarm(ctx context.Context, scanCfg *ScanConfig, minAge time.Duration, excludeIDs []string) (*PrewarmTarget, error) {
	storageID := config.AppConfig.StorageID
	pop := config.AppConfig.PrewarmPOP

	reprewarmAgeCutoff := time.Now().Add(-minAge)
	lockCutoff := time.Now().Add(-5 * time.Minute)

	baseCriteria := bson.M{
		"$or": bson.A{
			bson.M{
				"type":       models.MediaTypeVideo,
				"resolution": bson.M{"$in": VideoResolutions},
			},
			bson.M{
				"type":     models.MediaTypeThumbnail,
				"fileName": "sprite.vtt",
			},
		},
	}

	var reprewarmFilter bson.M
	if pop != "fra" {
		// Non-FRA: find media that FRA has but this POP hasn't, OR this POP prewarmed > minAge ago
		reprewarmFilter = bson.M{
			"prewarm.fra.prewarmAt": bson.M{"$exists": true} ,
			"$or": bson.A{
				// Category 1: Never completed prewarm on this POP (and not locked/processing recently)
				bson.M{
					"prewarm." + pop + ".prewarmAt": bson.M{"$exists": false},
					"$or": bson.A{
						bson.M{"prewarm." + pop: bson.M{"$exists": false}},
						bson.M{
							"prewarm." + pop + ".status":   "processing",
							"prewarm." + pop + ".lockedAt": bson.M{"$lt": lockCutoff},
						},
					},
				},
				// Category 2: Completed but old (and not locked/processing recently)
				bson.M{
					"prewarm." + pop + ".prewarmAt": bson.M{"$lt": reprewarmAgeCutoff},
					"$or": bson.A{
						bson.M{"prewarm." + pop + ".status": bson.M{"$ne": "processing"}},
						bson.M{
							"prewarm." + pop + ".status":   "processing",
							"prewarm." + pop + ".lockedAt": bson.M{"$lt": lockCutoff},
						},
					},
				},
			},
		}
	} else {
		// FRA: only re-prewarm media that this POP already prewarmed > minAge ago (and not locked/processing recently)
		reprewarmFilter = bson.M{
			"prewarm." + pop + ".prewarmAt": bson.M{"$lt": reprewarmAgeCutoff},
			"$or": bson.A{
				bson.M{"prewarm." + pop + ".status": bson.M{"$ne": "processing"}},
				bson.M{
					"prewarm." + pop + ".status":   "processing",
					"prewarm." + pop + ".lockedAt": bson.M{"$lt": lockCutoff},
				},
			},
		}
	}

	filter := bson.M{
		"deletedAt": nil,
		"$and": bson.A{
			baseCriteria,
			reprewarmFilter,
		},
	}
	if storageID != "" {
		filter["storageId"] = storageID
	}

	// Sort: for non-FRA, newest FRA prewarm first; for FRA, oldest first (re-prewarm)
	sortField := "prewarm." + pop + ".prewarmAt"
	sortDir := 1
	if pop != "fra" {
		sortField = "prewarm.fra.prewarmAt"
		sortDir = -1
	}

	return findAndLockTarget(ctx, scanCfg, filter, bson.D{{Key: sortField, Value: sortDir}}, excludeIDs)
}

// CountPending returns count of media without prewarm for this POP
func CountPending(ctx context.Context) (int64, int64) {
	storageID := config.AppConfig.StorageID
	pop := config.AppConfig.PrewarmPOP

	baseFilter := bson.M{
		"deletedAt": nil,
		"$or": bson.A{
			bson.M{
				"type":       models.MediaTypeVideo,
				"resolution": bson.M{"$in": VideoResolutions},
			},
			bson.M{
				"type":     models.MediaTypeThumbnail,
				"fileName": "sprite.vtt",
			},
		},
	}
	if storageID != "" {
		baseFilter["storageId"] = storageID
	}

	// Count no-prewarm for this POP
	noPrewarmFilter := copyFilter(baseFilter)
	noPrewarmFilter["prewarm."+pop] = bson.M{"$exists": false}
	noPrwm, _ := models.MediaModel.Col().CountDocuments(ctx, noPrewarmFilter)

	// Count total
	total, _ := models.MediaModel.Col().CountDocuments(ctx, baseFilter)

	return noPrwm, total
}

func copyFilter(f bson.M) bson.M {
	c := make(bson.M, len(f))
	for k, v := range f {
		c[k] = v
	}
	return c
}

// getSettingValue fetches a setting value from MongoDB settings collection
func getSettingValue(ctx context.Context, name string) string {
	var setting struct {
		Value interface{} `bson:"value"`
	}
	err := models.SettingModel.Col().FindOne(ctx, bson.M{"name": name}).Decode(&setting)
	if err != nil {
		return ""
	}
	if str, ok := setting.Value.(string); ok {
		return str
	}
	return ""
}
