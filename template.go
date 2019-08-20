package cypress

import (
	"errors"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	//ErrNoFile no file given for Creating a template
	ErrNoFile = errors.New("No template file")

	//SkinDefault default skin name
	SkinDefault = "default"
)

type templateFileInfo struct {
	file        string
	lastModifed time.Time
}

// TemplateConfigFunc shared templates config function
type TemplateConfigFunc func(*template.Template)

// SharedTemplateDetector detector used to check if the given
// path is a shared template or not
type SharedTemplateDetector func(string) bool

// TemplateManager manages the templates by groups and update template group on-demand
// based on the template file update timestamp
type TemplateManager struct {
	dir                    string
	lock                   *sync.RWMutex
	shared                 *template.Template
	templates              map[string]*template.Template
	fileLock               *sync.RWMutex
	files                  map[string]time.Time
	sharedFiles            []string
	refresher              *time.Ticker
	exitChan               chan bool
	configFunc             TemplateConfigFunc
	sharedTemplateDetector SharedTemplateDetector
}

// SkinSelector returns a skin name based on the request object
type SkinSelector interface {
	GetSkin(request *http.Request) string
}

// SkinSelectorFunc converts a function to SkinSelector interface
type SkinSelectorFunc func(*http.Request) string

// SkinManager a TemplateManager is mapped to a skin, skin manager manages TemplateManagers
// by names.
type SkinManager struct {
	defaultSkin *TemplateManager
	skins       map[string]*TemplateManager
	lock        *sync.RWMutex
	selector    SkinSelector
}

// NewTemplateManager creates a template manager for the given dir
func NewTemplateManager(dir, suffix string, refreshInterval time.Duration, configFunc TemplateConfigFunc, sharedTemplateDetector SharedTemplateDetector) *TemplateManager {
	dirs := make([]string, 0, 10)
	sharedFiles := make([]string, 0, 20)
	tmplFiles := make([]string, 0, 20)
	filesTime := make(map[string]time.Time)
	sharedDetector := sharedTemplateDetector
	if sharedDetector == nil {
		sharedDetector = func(path string) bool {
			return strings.HasPrefix(path, "shared/") || strings.Contains(path, "/shared/")
		}
	}

	// scan dir for all template files
	dirs = append(dirs, dir)
	for len(dirs) > 0 {
		current := dirs[0]
		dirs = dirs[1:]
		if !strings.HasSuffix(current, "/") {
			current = current + "/"
		}

		zap.L().Info("scan for template files", zap.String("dir", current), zap.String("suffix", suffix))
		files, err := ioutil.ReadDir(current)
		if err != nil {
			zap.L().Error("failed to scan directory for template files", zap.String("dir", current), zap.Error(err))
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				dirs = append(dirs, current+file.Name()+"/")
			} else if strings.HasSuffix(file.Name(), suffix) {
				zap.L().Info("template file found", zap.String("file", file.Name()))
				path := current + file.Name()
				filesTime[path] = file.ModTime()
				if sharedDetector(path) {
					sharedFiles = append(sharedFiles, path)
				} else {
					tmplFiles = append(tmplFiles, path)
				}
			}
		}
	}

	shared := template.New("cypress$shared$root")
	if configFunc != nil {
		configFunc(shared)
	}

	if len(sharedFiles) > 0 {
		t, err := shared.ParseFiles(sharedFiles...)
		if err != nil {
			zap.L().Error("failed to parse files into shared template, shared will be defaulted to empty", zap.Error(err))
		} else {
			shared = t
		}
	}

	templates := make(map[string]*template.Template)
	for _, file := range tmplFiles {
		key := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(file, filepath.Ext(file)), dir), "/\\")
		tmpl, err := shared.Clone()
		if err != nil {
			zap.L().Error("failed to clone shared template", zap.Error(err), zap.String("file", file))
		} else {
			tmpl, err = tmpl.ParseFiles(file)
			if err != nil {
				zap.L().Error("failed to parse template file", zap.Error(err), zap.String("file", file))
			} else {
				templates[key] = tmpl
			}
		}
	}

	mgr := &TemplateManager{
		dir:                    dir,
		lock:                   &sync.RWMutex{},
		shared:                 shared,
		templates:              templates,
		fileLock:               &sync.RWMutex{},
		files:                  filesTime,
		sharedFiles:            sharedFiles,
		refresher:              time.NewTicker(refreshInterval),
		exitChan:               make(chan bool),
		configFunc:             configFunc,
		sharedTemplateDetector: sharedDetector,
	}

	go func() {
		for {
			select {
			case <-mgr.refresher.C:
				mgr.refreshTemplates()
				break
			case <-mgr.exitChan:
				return
			}
		}
	}()
	return mgr
}

// Close closes the template manager and release all resources
func (manager *TemplateManager) Close() {
	manager.exitChan <- true
	manager.refresher.Stop()
	close(manager.exitChan)
}

// GetTemplate retrieve a template with the specified name from
// template manager, this returns the template and a boolean value
// to indicate if the template is found or not
func (manager *TemplateManager) GetTemplate(name string) (*template.Template, bool) {
	manager.lock.RLock()
	defer manager.lock.RUnlock()
	result, ok := manager.templates[name]
	return result, ok
}

func (manager *TemplateManager) refreshTemplates() {
	files := make([]string, 0, len(manager.files))
	func() {
		manager.fileLock.RLock()
		defer manager.fileLock.RUnlock()
		for key := range manager.files {
			files = append(files, key)
		}
	}()

	reparseAll := false
	filesToReparse := make([]string, 0, 20)
	for _, file := range files {
		var t time.Time
		var ok bool

		stat, err := os.Stat(file)
		if err != nil {
			zap.L().Error("failed to get file meta info", zap.Error(err), zap.String("file", file))
			continue
		}

		func() {
			manager.fileLock.RLock()
			defer manager.fileLock.RUnlock()
			t, ok = manager.files[file]
		}()

		if !ok {
			zap.L().Error("failed to see the file time", zap.String("file", file))
			continue
		}

		if t.Before(stat.ModTime()) {
			func() {
				manager.fileLock.Lock()
				defer manager.fileLock.Unlock()
				manager.files[file] = stat.ModTime()
			}()

			if manager.sharedTemplateDetector(file) {
				// a shared template file has been changed, so we need to re-parse
				// all templates, let the loop continue to allow detect and update
				// last updated time for all files
				reparseAll = true
			} else {
				// only the given template need to be updated
				filesToReparse = append(filesToReparse, file)
			}
		}
	}

	if reparseAll && len(manager.sharedFiles) > 0 {
		shared := template.New("cypress$shared$root")
		if manager.configFunc != nil {
			manager.configFunc(shared)
		}

		t, err := shared.ParseFiles(manager.sharedFiles...)
		if err != nil {
			manager.shared = shared
			zap.L().Error("failed to parse files into shared template, shared will be defaulted to empty", zap.Error(err))
		} else {
			manager.shared = t
		}

		filesToReparse = files
	}

	for _, file := range filesToReparse {
		if !manager.sharedTemplateDetector(file) {
			name := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(file, filepath.Ext(file)), manager.dir), "/\\")
			tmpl, err := manager.shared.Clone()
			if err != nil {
				zap.L().Error("failed to clone shared template", zap.Error(err), zap.String("file", file))
			} else {
				tmpl, err = tmpl.ParseFiles(file)
				if err != nil {
					zap.L().Error("failed to parse template file", zap.Error(err), zap.String("file", file))
				} else {
					func() {
						manager.lock.Lock()
						defer manager.lock.Unlock()
						manager.templates[name] = tmpl
					}()
					zap.L().Info("template file reparsed", zap.String("file", file))
				}
			}
		}
	}
}

// NewSkinManager creates a skin manager object
func NewSkinManager(defaultSkin *TemplateManager) *SkinManager {
	return &SkinManager{defaultSkin, make(map[string]*TemplateManager), &sync.RWMutex{}, nil}
}

// AddSkin adds a skin
func (skinMgr *SkinManager) AddSkin(name string, tmplMgr *TemplateManager) {
	skinMgr.lock.Lock()
	defer skinMgr.lock.Unlock()
	skinMgr.skins[name] = tmplMgr
}

// RemoveSkin removes a skin
func (skinMgr *SkinManager) RemoveSkin(name string) {
	skinMgr.lock.Lock()
	defer skinMgr.lock.Unlock()
	delete(skinMgr.skins, name)
}

// GetDefaultSkin gets the default skin
func (skinMgr *SkinManager) GetDefaultSkin() *TemplateManager {
	skinMgr.lock.RLock()
	defer skinMgr.lock.RUnlock()
	return skinMgr.defaultSkin
}

// GetSkinOrDefault gets the skin with the given name if it's not
// found return the default skin
func (skinMgr *SkinManager) GetSkinOrDefault(name string) *TemplateManager {
	skinMgr.lock.RLock()
	defer skinMgr.lock.RUnlock()
	tmplMgr, ok := skinMgr.skins[name]
	if !ok {
		return skinMgr.defaultSkin
	}

	return tmplMgr
}

// GetSkin find skin by name
func (skinMgr *SkinManager) GetSkin(name string) (*TemplateManager, bool) {
	skinMgr.lock.RLock()
	defer skinMgr.lock.RUnlock()
	tmplMgr, ok := skinMgr.skins[name]
	return tmplMgr, ok
}

// ApplySelector find skin by applying selector, if selector is not configured
// default skin will be returned, if the skin name returned by the selector cannot
// be found, this returns nil
func (skinMgr *SkinManager) ApplySelector(request *http.Request) (*TemplateManager, string) {
	if skinMgr.selector == nil {
		return skinMgr.GetDefaultSkin(), SkinDefault
	}

	skinMgr.lock.RLock()
	defer skinMgr.lock.RUnlock()
	name := skinMgr.selector.GetSkin(request)
	skin, _ := skinMgr.GetSkin(name)
	return skin, name
}

// WithSelector sets the skin selector for the skin manager
func (skinMgr *SkinManager) WithSelector(selector SkinSelector) {
	skinMgr.lock.Lock()
	defer skinMgr.lock.Unlock()
	skinMgr.selector = selector
}
