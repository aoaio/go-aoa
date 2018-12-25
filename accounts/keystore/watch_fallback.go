// +build ios linux,arm64 windows !darwin,!freebsd,!linux,!netbsd,!solaris

package keystore

type watcher struct{ running bool }

func newWatcher(*accountCache) *watcher { return new(watcher) }
func (*watcher) start()                 {}
func (*watcher) close()                 {}
