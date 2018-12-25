package keystore

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Aurorachain/go-Aurora/log"
	set "gopkg.in/fatih/set.v0"
)

type fileCache struct {
	all     *set.SetNonTS
	lastMod time.Time
	mu      sync.RWMutex
}

func (fc *fileCache) scan(keyDir string) (set.Interface, set.Interface, set.Interface, error) {
	t0 := time.Now()

	files, err := ioutil.ReadDir(keyDir)
	if err != nil {
		return nil, nil, nil, err
	}
	t1 := time.Now()

	fc.mu.Lock()
	defer fc.mu.Unlock()

	all := set.NewNonTS()
	mods := set.NewNonTS()

	var newLastMod time.Time
	for _, fi := range files {

		path := filepath.Join(keyDir, fi.Name())
		if skipKeyFile(fi) {
			log.Trace("Ignoring file on account scan", "path", path)
			continue
		}

		all.Add(path)

		modified := fi.ModTime()
		if modified.After(fc.lastMod) {
			mods.Add(path)
		}
		if modified.After(newLastMod) {
			newLastMod = modified
		}
	}
	t2 := time.Now()

	deletes := set.Difference(fc.all, all)
	creates := set.Difference(all, fc.all)
	updates := set.Difference(mods, creates)

	fc.all, fc.lastMod = all, newLastMod
	t3 := time.Now()

	log.Debug("FS scan times", "list", t1.Sub(t0), "set", t2.Sub(t1), "diff", t3.Sub(t2))
	return creates, deletes, updates, nil
}

func skipKeyFile(fi os.FileInfo) bool {

	if strings.HasSuffix(fi.Name(), "~") || strings.HasPrefix(fi.Name(), ".") {
		return true
	}

	if fi.IsDir() || fi.Mode()&os.ModeType != 0 {
		return true
	}
	return false
}
