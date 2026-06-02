package models

// ─── User Roles ──────────────────────────────────────────────────────

const (
	UserRoleUser       = "user"
	UserRoleAdmin      = "admin"
	UserRoleSuperAdmin = "super_admin"
	UserRoleDeveloper  = "developer"
)

// ─── Workspace Member Roles ──────────────────────────────────────────

const (
	WorkspaceMemberRoleOwner  = "owner"
	WorkspaceMemberRoleAdmin  = "admin"
	WorkspaceMemberRoleMember = "member"
	WorkspaceMemberRoleViewer = "viewer"
)

// ─── Workspace Statuses ──────────────────────────────────────────────

const (
	WorkspaceStatusPending   = "pending"
	WorkspaceStatusActive    = "active"
	WorkspaceStatusSuspended = "suspended"
)

// ─── Workspace Member Statuses ───────────────────────────────────────

const (
	MemberStatusPending  = "pending"
	MemberStatusActive   = "active"
	MemberStatusRejected = "rejected"
)

// ─── Plan Types ──────────────────────────────────────────────────────

const (
	PlanTypeHobby      = "hobby"
	PlanTypePro        = "pro"
	PlanTypeBusiness   = "business"
	PlanTypeEnterprise = "enterprise"
)

// ─── File Types ──────────────────────────────────────────────────────

const (
	FileTypeFolder = "folder"
	FileTypeVideo  = "video"
	FileTypeImage  = "image"
	FileTypeOther  = "other"
	FileTypeSpace  = "space"
)

// ─── File Statuses ───────────────────────────────────────────────────

const (
	FileStatusWaiting    = "waiting"
	FileStatusProcessing = "processing"
	FileStatusReady      = "ready"
	FileStatusError      = "error"
)

// ─── File Source Types ───────────────────────────────────────────────

const (
	FileSourceTypeUpload  = "upload"
	FileSourceTypeYoutube = "youtube"
	FileSourceTypeVimeo   = "vimeo"
	FileSourceTypeOther   = "other"
)

// ─── Media Types ─────────────────────────────────────────────────────

const (
	MediaTypeVideo     = "video"
	MediaTypeAudio     = "audio"
	MediaTypeSubtitle  = "subtitle"
	MediaTypeThumbnail = "thumbnail"
	MediaTypeImage     = "image"
	MediaTypeDocument  = "document"
	MediaTypeOther     = "other"
)

// ─── Ingest Source Types ─────────────────────────────────────────────

const (
	IngestSourceTypeUpload   = "upload"
	IngestSourceTypeRemote   = "remote"
	IngestSourceTypeGDrive   = "gdrive"
	IngestSourceTypeS3Import = "s3_import"
)

// ─── Storage Types ───────────────────────────────────────────────────

const (
	StorageTypeLocal = "local"
	StorageTypeS3    = "s3"
)

// ─── Storage Statuses ────────────────────────────────────────────────

const (
	StorageStatusOnline      = "online"
	StorageStatusOffline     = "offline"
	StorageStatusError       = "error"
	StorageStatusMaintenance = "maintenance"
)

// ─── Storage Accepts ─────────────────────────────────────────────────

const (
	StorageAcceptUpload = "upload"
	StorageAcceptVideo  = "video"
	StorageAcceptImage  = "image"
	StorageAcceptOther  = "other"
)

// ─── Resolution ──────────────────────────────────────────────────────

const (
	ResolutionOriginal = "original"
	ResolutionTrailer  = "trailer"
	Resolution1080     = "1080"
	Resolution720      = "720"
	Resolution480      = "480"
	Resolution360      = "360"
)

// ─── Image Resolution ─────────────────────────────────────────────────

const (
	ResolutionPoster  = "poster"
	ResolutionGallery = "gallery"
)

// ─── Domain Statuses ─────────────────────────────────────────────────

const (
	DomainStatusPending = "pending"
	DomainStatusActive  = "active"
	DomainStatusFailed  = "failed"
	DomainStatusExpired = "expired"
)

// ─── DMCA Statuses ───────────────────────────────────────────────────

const (
	DmcaStatusPending       = "pending"
	DmcaStatusReviewing     = "reviewing"
	DmcaStatusApproved      = "approved"
	DmcaStatusRejected      = "rejected"
	DmcaStatusCounterNotice = "counter_notice"
)

// ─── DMCA Types ──────────────────────────────────────────────────────

const (
	DmcaTypeCopyright = "copyright"
	DmcaTypeTrademark = "trademark"
	DmcaTypeOther     = "other"
)
