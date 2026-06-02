package models

import (
	"time"

	"github.com/zergolf1994/goose"
)

// AdsContent ข้อมูลเนื้อหาโฆษณา (polymorphic ตาม type)
type AdsContent struct {
	// Video ad
	Mp4URL      *string `bson:"mp4Url,omitempty" json:"mp4Url,omitempty"`
	SkipSeconds *int    `bson:"skipSeconds,omitempty" json:"skipSeconds,omitempty"`

	// Image ad
	ImageURL *string  `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"`
	ShowOn   []string `bson:"showOn,omitempty" json:"showOn,omitempty"`

	// Shared
	WebsiteURL *string `bson:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`

	// JavaScript ad
	Script *string `bson:"script,omitempty" json:"script,omitempty"`
}

// Ads คือ record โฆษณาของ workspace
// Collection: "ads" | _id: String (UUID)
type Ads struct {
	ID        string      `bson:"_id" json:"id" goose:"required,default:uuid"`
	SpaceID   string      `bson:"spaceId" json:"spaceId" goose:"ref:workspaces,index,required"`
	CreatorID string      `bson:"creatorId" json:"creatorId" goose:"ref:user,required"`
	Name      string      `bson:"name" json:"name" goose:"required"`
	Type      string      `bson:"type" json:"type" goose:"required"`
	Status    string      `bson:"status" json:"status" goose:"default:active"`
	Content   *AdsContent `bson:"content,omitempty" json:"content,omitempty"`
	CreatedAt time.Time   `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt time.Time   `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// AdsModel goose model สำหรับ collection "ads"
var AdsModel = goose.NewModel[Ads]("ads")
