package models

// effectiveFileId returns ClonedFrom if set, otherwise FileID.
// Cloned media shares the original file, so paths must resolve to the source.
func (m *Media) effectiveFileId() string {
	if m.ClonedFrom != nil && *m.ClonedFrom != "" {
		return *m.ClonedFrom
	}
	if m.FileID != nil {
		return *m.FileID
	}
	return ""
}

// GetFilePath returns the expected file path on storage
// Structure: {storage.path}/{fileId}/{file_name}
func (m *Media) GetFilePath(storagePath string) string {
	fileName := ""
	if m.FileName != nil {
		fileName = *m.FileName
	}
	return storagePath + "/" + m.effectiveFileId() + "/" + fileName
}

// GetFolderPath returns the folder path containing the file
func (m *Media) GetFolderPath(storagePath string) string {
	return storagePath + "/" + m.effectiveFileId()
}
