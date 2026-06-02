package models

// IsFolder checks if the file is a folder
func (f *File) IsFolder() bool {
	return f.Type == FileTypeFolder
}

// IsVideo checks if the file is a video
func (f *File) IsVideo() bool {
	return f.Type == FileTypeVideo
}

// IsSpace checks if the file is a space
func (f *File) IsSpace() bool {
	return f.Type == FileTypeSpace
}

// IsTrashed checks if the file has been trashed
func (f *File) IsTrashed() bool {
	return f.Metadata != nil && f.Metadata.TrashedAt != nil
}

// IsDeleted checks if the file has been soft-deleted
func (f *File) IsDeleted() bool {
	return f.Metadata != nil && f.Metadata.DeletedAt != nil
}

// IsReady checks if the file status is ready
func (f *File) IsReady() bool {
	return f.Status == FileStatusReady
}
