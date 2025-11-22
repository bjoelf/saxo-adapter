package saxo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileTokenStorage implements TokenStorage interface using file-based persistence
type FileTokenStorage struct {
	basePath string
}

// NewTokenStorage creates a new file-based token storage
// Stores tokens in the data/ directory by default
func NewTokenStorage() TokenStorage {
	basePath := os.Getenv("TOKEN_STORAGE_PATH")
	if basePath == "" {
		basePath = "data" // Default to data/ directory
	}

	// Create directory if it doesn't exist
	os.MkdirAll(basePath, 0700)

	return &FileTokenStorage{
		basePath: basePath,
	}
}

// SaveToken saves token to file
func (f *FileTokenStorage) SaveToken(filename string, token *TokenInfo) error {
	filePath := filepath.Join(f.basePath, filename)

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Write with restricted permissions (owner only)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// LoadToken loads token from file
func (f *FileTokenStorage) LoadToken(filename string) (*TokenInfo, error) {
	filePath := filepath.Join(f.basePath, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("token file not found: %s", filename)
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	return &token, nil
}

// DeleteToken deletes token file
func (f *FileTokenStorage) DeleteToken(filename string) error {
	filePath := filepath.Join(f.basePath, filename)

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	return nil
}
