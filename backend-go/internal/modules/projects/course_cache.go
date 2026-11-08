package projects

import (
	"os"
	"sync"
)

// Cache only immutable file bytes and revalidate file identity and metadata on
// every access. Moving/deleting/recreating a workspace therefore invalidates
// naturally, including filesystem changes outside the application.
const courseCacheLimit = 32 << 20

var courseFiles = struct {
	sync.Mutex
	entries map[string]courseFile
	bytes   int
	clock   uint64
}{entries: map[string]courseFile{}}

type courseFile struct {
	info  os.FileInfo
	bytes []byte
	used  uint64
}

func readCourseFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	courseFiles.Lock()
	cached, ok := courseFiles.entries[path]
	if ok && os.SameFile(cached.info, info) && cached.info.Size() == info.Size() && cached.info.ModTime() == info.ModTime() {
		courseFiles.clock++
		cached.used = courseFiles.clock
		courseFiles.entries[path] = cached
		b := append([]byte(nil), cached.bytes...)
		courseFiles.Unlock()
		return b, nil
	}
	courseFiles.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	courseFiles.Lock()
	defer courseFiles.Unlock()
	if previous, ok := courseFiles.entries[path]; ok {
		courseFiles.bytes -= len(previous.bytes)
		delete(courseFiles.entries, path)
	}
	if len(b) <= courseCacheLimit {
		for courseFiles.bytes+len(b) > courseCacheLimit {
			oldest := ""
			var age uint64 = ^uint64(0)
			for key, value := range courseFiles.entries {
				if value.used < age {
					oldest = key
					age = value.used
				}
			}
			courseFiles.bytes -= len(courseFiles.entries[oldest].bytes)
			delete(courseFiles.entries, oldest)
		}
		courseFiles.clock++
		courseFiles.entries[path] = courseFile{info, append([]byte(nil), b...), courseFiles.clock}
		courseFiles.bytes += len(b)
	}
	return b, nil
}
