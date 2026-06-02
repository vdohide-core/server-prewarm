package models

import (
	"time"

	"github.com/zergolf1994/goose"
)

// Worker statuses
const (
	WorkerStatusIdle    = "idle"
	WorkerStatusBusy    = "busy"
	WorkerStatusOffline = "offline"
)

// Worker types
const (
	WorkerTypeDownload  = "download"
	WorkerTypeTranscode = "transcode"
)


// WorkerSystemInfo holds system resource metrics reported by the worker.
type WorkerSystemInfo struct {
	DiskTotal  int64   `bson:"diskTotal,omitempty" json:"diskTotal,omitempty"`   // bytes
	DiskUsed   int64   `bson:"diskUsed,omitempty" json:"diskUsed,omitempty"`     // bytes
	DiskFree   int64   `bson:"diskFree,omitempty" json:"diskFree,omitempty"`     // bytes
	MemTotal   int64   `bson:"memTotal,omitempty" json:"memTotal,omitempty"`     // bytes
	MemUsed    int64   `bson:"memUsed,omitempty" json:"memUsed,omitempty"`       // bytes
	CPUPercent float64 `bson:"cpuPercent,omitempty" json:"cpuPercent,omitempty"` // 0-100
}

// Worker represents a download worker that reports heartbeats.
// Collection: "workers" | _id: String (UUID)
type Worker struct {
	ID          string            `bson:"_id" json:"id" goose:"required,default:uuid"`
	WorkerID    string            `bson:"workerId" json:"workerId" goose:"unique"` // hostname@n
	Hostname    string            `bson:"hostname" json:"hostname" goose:"index"`
	IP          string            `bson:"ip" json:"ip"`
	PID         int               `bson:"pid" json:"pid"`
	Enable      bool              `bson:"enable" json:"enable"`                 // false = stop accepting jobs
	Type        string            `bson:"type,omitempty" json:"type,omitempty"` // download, transcode
	Status      string            `bson:"status" json:"status"`                 // idle, busy, offline
	ActiveJobs  int               `bson:"activeJobs" json:"activeJobs"`
	MaxJobs     int               `bson:"maxJobs" json:"maxJobs"`
	System      *WorkerSystemInfo `bson:"system,omitempty" json:"system,omitempty"`
	HeartbeatAt time.Time         `bson:"heartbeatAt" json:"heartbeatAt" goose:"index"`
	CreatedAt   time.Time         `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt   time.Time         `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// WorkerModel is the goose model for the "workers" collection.
var WorkerModel = goose.NewModel[Worker]("workers")
