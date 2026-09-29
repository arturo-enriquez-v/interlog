package manager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"interlog/internal/engine"
	"interlog/internal/models"
)

type LogManager struct {
	mu             sync.RWMutex
	uploadsDir     string
	rootDir        string
	activeFileName string
	cache          map[string]*engine.IndexedLog
	fileMetadata   map[string]models.FileMeta
}

func NewLogManager(rootDir, uploadsDir string) *LogManager {
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		fmt.Printf("Advertencia al crear uploadsDir: %v\n", err)
	}
	return &LogManager{
		uploadsDir:   uploadsDir,
		rootDir:      rootDir,
		cache:        make(map[string]*engine.IndexedLog),
		fileMetadata: make(map[string]models.FileMeta),
	}
}

func (m *LogManager) InitActive(defaultFile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if defaultFile != "" {
		if _, err := os.Stat(defaultFile); err == nil {
			base := filepath.Base(defaultFile)
			m.activeFileName = base
			return m.loadAndIndexLocked(defaultFile, base, false)
		}
	}

	files := m.scanFilesLocked()
	if len(files) > 0 {
		m.activeFileName = files[0].Name
		return m.loadAndIndexLocked(files[0].Path, files[0].Name, files[0].IsUploaded)
	}

	return nil
}

func (m *LogManager) GetActiveIndexedLog() (*engine.IndexedLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeFileName == "" {
		files := m.scanFilesLocked()
		if len(files) == 0 {
			return nil, fmt.Errorf("no hay archivos de log registrados")
		}
		m.activeFileName = files[0].Name
	}

	if data, exists := m.cache[m.activeFileName]; exists {
		return data, nil
	}

	path, isUploaded := m.resolveFilePath(m.activeFileName)
	if path == "" {
		return nil, fmt.Errorf("archivo '%s' no encontrado", m.activeFileName)
	}

	if err := m.loadAndIndexLocked(path, m.activeFileName, isUploaded); err != nil {
		return nil, err
	}

	return m.cache[m.activeFileName], nil
}

func (m *LogManager) SetActiveFile(name string) (*engine.IndexedLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	base := filepath.Base(name)
	path, isUploaded := m.resolveFilePath(base)
	if path == "" {
		return nil, fmt.Errorf("archivo '%s' no encontrado", base)
	}

	m.activeFileName = base

	if data, exists := m.cache[base]; exists {
		return data, nil
	}

	if err := m.loadAndIndexLocked(path, base, isUploaded); err != nil {
		return nil, err
	}

	return m.cache[base], nil
}

func (m *LogManager) ListFiles() []models.FileMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()

	files := m.scanFilesLocked()
	for i := range files {
		files[i].IsActive = (files[i].Name == m.activeFileName)
		if cached, exists := m.cache[files[i].Name]; exists {
			files[i].Parsed = true
			files[i].TotalLines = cached.Summary.TotalLines
			files[i].ErrorCount = cached.Summary.RealErrorsCount
			files[i].Duration = cached.Summary.Duration
		}
	}
	return files
}

func (m *LogManager) SaveUploadedStream(filename string, r io.Reader) (*models.FileMeta, error) {
	cleanName := filepath.Base(filename)
	if cleanName == "" || cleanName == "." {
		cleanName = fmt.Sprintf("log_%d.log", time.Now().Unix())
	}

	destPath := filepath.Join(m.uploadsDir, cleanName)

	out, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("error al crear archivo en disco: %w", err)
	}
	defer out.Close()

	// Stream with 2MB buffer to handle 7GB+ files smoothly with constant RAM
	buf := make([]byte, 2*1024*1024)
	if _, err := io.CopyBuffer(out, r, buf); err != nil {
		return nil, fmt.Errorf("error al guardar contenido: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.activeFileName = cleanName
	if err := m.loadAndIndexLocked(destPath, cleanName, true); err != nil {
		return nil, fmt.Errorf("archivo guardado pero falló el análisis: %w", err)
	}

	cached := m.cache[cleanName]
	meta := models.FileMeta{
		Name:       cleanName,
		Path:       destPath,
		SizeMB:     cached.FileSizeMB,
		ModTime:    time.Now().Format("2006-01-02 15:04:05"),
		IsActive:   true,
		IsUploaded: true,
		TotalLines: cached.Summary.TotalLines,
		ErrorCount: cached.Summary.RealErrorsCount,
		Duration:   cached.Summary.Duration,
		Parsed:     true,
	}

	return &meta, nil
}

func (m *LogManager) RegisterLocalPath(path string) (*models.FileMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo acceder a la ruta '%s': %w", path, err)
	}

	cleanName := filepath.Base(path)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.activeFileName = cleanName
	if err := m.loadAndIndexLocked(path, cleanName, false); err != nil {
		return nil, fmt.Errorf("error al analizar archivo local: %w", err)
	}

	cached := m.cache[cleanName]
	meta := models.FileMeta{
		Name:       cleanName,
		Path:       path,
		SizeMB:     float64(fi.Size()) / (1024 * 1024),
		ModTime:    fi.ModTime().Format("2006-01-02 15:04:05"),
		IsActive:   true,
		IsUploaded: false,
		TotalLines: cached.Summary.TotalLines,
		ErrorCount: cached.Summary.RealErrorsCount,
		Duration:   cached.Summary.Duration,
		Parsed:     true,
	}
	m.fileMetadata[cleanName] = meta

	return &meta, nil
}

func (m *LogManager) DeleteFile(filename string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanName := filepath.Base(filename)
	path := filepath.Join(m.uploadsDir, cleanName)

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("solo se pueden eliminar archivos subidos a uploads/")
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	delete(m.cache, cleanName)
	delete(m.fileMetadata, cleanName)

	if m.activeFileName == cleanName {
		m.activeFileName = ""
		files := m.scanFilesLocked()
		if len(files) > 0 {
			m.activeFileName = files[0].Name
			_ = m.loadAndIndexLocked(files[0].Path, files[0].Name, files[0].IsUploaded)
		}
	}

	return nil
}

func (m *LogManager) resolveFilePath(name string) (string, bool) {
	uploadPath := filepath.Join(m.uploadsDir, name)
	if _, err := os.Stat(uploadPath); err == nil {
		return uploadPath, true
	}

	rootPath := filepath.Join(m.rootDir, name)
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath, false
	}

	if meta, exists := m.fileMetadata[name]; exists {
		if _, err := os.Stat(meta.Path); err == nil {
			return meta.Path, false
		}
	}

	return "", false
}

func (m *LogManager) scanFilesLocked() []models.FileMeta {
	var list []models.FileMeta
	seen := make(map[string]bool)

	// Scan uploads
	entries, _ := os.ReadDir(m.uploadsDir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".txt") {
			fullPath := filepath.Join(m.uploadsDir, name)
			if fi, err := entry.Info(); err == nil {
				sizeMB := float64(fi.Size()) / (1024 * 1024)
				list = append(list, models.FileMeta{
					Name:       name,
					Path:       fullPath,
					SizeMB:     sizeMB,
					ModTime:    fi.ModTime().Format("2006-01-02 15:04:05"),
					IsUploaded: true,
				})
				seen[name] = true
			}
		}
	}

	// Scan root directory
	rootEntries, _ := os.ReadDir(m.rootDir)
	for _, entry := range rootEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if seen[name] {
			continue
		}
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".txt") {
			fullPath := filepath.Join(m.rootDir, name)
			if fi, err := entry.Info(); err == nil {
				sizeMB := float64(fi.Size()) / (1024 * 1024)
				list = append(list, models.FileMeta{
					Name:       name,
					Path:       fullPath,
					SizeMB:     sizeMB,
					ModTime:    fi.ModTime().Format("2006-01-02 15:04:05"),
					IsUploaded: false,
				})
				seen[name] = true
			}
		}
	}

	// Add registered custom local paths
	for name, meta := range m.fileMetadata {
		if !seen[name] {
			list = append(list, meta)
			seen[name] = true
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime > list[j].ModTime
	})

	return list
}

func (m *LogManager) loadAndIndexLocked(path, name string, isUploaded bool) error {
	fmt.Printf("[ENGINE] Indexando '%s' en streaming de bajo consumo...\n", path)
	idxLog, err := engine.IndexFile(path)
	if err != nil {
		return err
	}

	m.cache[name] = idxLog
	return nil
}
