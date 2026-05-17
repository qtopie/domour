package config

import (
	"log"
	"sync"

	"github.com/fsnotify/fsnotify"
)

var (
	watchOnce sync.Once
	watchErr  error
	watchMu   sync.Mutex
	watchSubs []func()
)

func WatchConfig(callback func()) {
	watchMu.Lock()
	watchSubs = append(watchSubs, callback)
	watchMu.Unlock()

	watchOnce.Do(func() {
		path, err := DomourConfigPath()
		if err != nil {
			watchErr = err
			return
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			watchErr = err
			return
		}

		go func() {
			defer watcher.Close()
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}
					if event.Op&fsnotify.Write == fsnotify.Write {
						log.Printf("[Config] Config file changed: %s, reloading...", event.Name)
						ReloadDomourConfig()
						notifySubs()
					}
				case err, ok := <-watcher.Errors:
					if !ok {
						return
					}
					log.Printf("[Config] Watcher error: %v", err)
				}
			}
		}()

		err = watcher.Add(path)
		if err != nil {
			watchErr = err
		}
	})
}

func notifySubs() {
	watchMu.Lock()
	subs := make([]func(), len(watchSubs))
	copy(subs, watchSubs)
	watchMu.Unlock()

	for _, sub := range subs {
		if sub != nil {
			go sub()
		}
	}
}
