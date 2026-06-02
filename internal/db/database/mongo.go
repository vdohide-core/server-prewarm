package database

import (
	"context"
	"log"
	"time"

	"server-prewarm/internal/config"
	"server-prewarm/internal/db/models"
	"github.com/zergolf1994/goose"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Connect establishes a connection to MongoDB via goose ODM.
func Connect() error {
	uri := config.AppConfig.MongoURI
	if err := goose.Connect(uri); err != nil {
		return err
	}
	EnsureIndexes()
	return nil
}

// Disconnect closes the MongoDB connection.
func Disconnect() {
	if goose.Client() != nil {
		if err := goose.Close(); err != nil {
			log.Printf("⚠️ Error disconnecting from MongoDB: %v", err)
		} else {
			log.Println("🔌 Disconnected from MongoDB")
		}
	}
}

// DB returns the database instance (delegates to goose).
func DB() *mongo.Database {
	return goose.DB()
}

// ─── Indexes ──────────────────────────────────────────────────

// EnsureIndexes creates required indexes for concurrency safety.
func EnsureIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vpCol := models.VideoProcessModel.Col()

	// Drop stale indexes
	vpCol.Indexes().DropOne(ctx, "postId_1")
	vpCol.Indexes().DropOne(ctx, "fileId_1")

	// Clean up duplicate fileId records before creating unique index
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$fileId"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "ids", Value: bson.D{{Key: "$push", Value: "$_id"}}},
		}}},
		{{Key: "$match", Value: bson.D{{Key: "count", Value: bson.D{{Key: "$gt", Value: 1}}}}}},
	}
	cursor, err := vpCol.Aggregate(ctx, pipeline)
	if err == nil {
		type DupResult struct {
			FileID string   `bson:"_id"`
			Count  int      `bson:"count"`
			IDs    []string `bson:"ids"`
		}
		for cursor.Next(ctx) {
			var dup DupResult
			if cursor.Decode(&dup) == nil && len(dup.IDs) > 1 {
				deleteIDs := dup.IDs[1:]
				vpCol.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": deleteIDs}})
				log.Printf("🧹 Removed %d duplicate video_process for fileId %s", len(deleteIDs), dup.FileID)
			}
		}
		cursor.Close(ctx)
	}

	// Unique index on video_process.fileId
	_, err = vpCol.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "fileId", Value: 1}},
		Options: options.Index().SetUnique(true).SetSparse(true),
	})
	if err != nil {
		log.Printf("⚠️  Index creation warning: %v", err)
	} else {
		log.Printf("✅ Unique index on video_process.fileId ensured")
	}
}
