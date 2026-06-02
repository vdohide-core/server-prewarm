package models

import "fmt"

// GetPath returns the storage base path.
func (s *Storage) GetPath() string {
	if s.Local != nil && s.Local.Path != "" {
		return s.Local.Path
	}
	return "/home/files"
}

// GetHost returns the storage host.
func (s *Storage) GetHost() string {
	if s.Local != nil {
		return s.Local.Host
	}
	return ""
}

// GetPort returns the internal port based on storage type.
func (s *Storage) GetPort() int {
	if s.Local != nil {
		return s.Local.Port
	}
	return 0
}

// GetHostPort returns "host:port" for internal HTTP access.
func (s *Storage) GetHostPort() string {
	host := s.GetHost()
	if host == "" {
		return ""
	}
	port := s.GetPort()
	if port > 0 {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

// HasSSHCredentials checks if storage has valid SSH credentials.
func (s *Storage) HasSSHCredentials() bool {
	if s.Local == nil || s.Local.SSH == nil {
		return false
	}
	return s.Local.SSH.Username != "" && s.Local.SSH.Password != "" && s.Local.SSH.Port > 0
}

// IsOnline checks if storage is enabled and online.
func (s *Storage) IsOnline() bool {
	return s.Enable && s.Status == StorageStatusOnline
}
