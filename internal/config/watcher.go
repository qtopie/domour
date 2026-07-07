package config

import (
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 500 * time.Millisecond

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

			var (
				debounce     *time.Timer
				debounceMu   sync.Mutex
			)

			triggerReload := func(name string) {
				debounceMu.Lock()
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(debounceInterval, func() {
					log.Printf("[Config] Config file changed: %s, reloading...", name)
					ReloadDomourConfig()
					notifySubs()
				})
				debounceMu.Unlock()
			}

			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						debounceMu.Lock()
						if debounce != nil {
							debounce.Stop()
						}
						debounceMu.Unlock()
						return
					}
					if event.Op&fsnotify.Write == fsnotify.Write {
						triggerReload(event.Name)
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
