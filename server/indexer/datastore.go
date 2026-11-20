// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/asciimoo/nogon/config"
	"github.com/klauspost/compress/zstd"

	"github.com/rs/zerolog/log"
)

const (
	dataDirName   = "data"
	htmlSubdir    = "html"
	faviconSubdir = "favicon"
	gzipExtension = ".gz"
	zstdExtension = ".zst"
)

// dataStore manages the content-addressed file-system store for HTML and
// favicon data. A fixed pool of 256 per-shard RWMutexes (indexed by the first
// byte of the SHA-256 key) serialises concurrent operations on files in the
// same shard while allowing fully parallel access across different shards.
//
// The struct is shared between the live indexer and the temporary indexer
// created during reindex so that both share locks and pending references.
type dataStore struct {
	dir         string
	compression string
	shards      [256]dataShard
}

type dataReference struct {
	subdir string
	key    string
}

type dataShard struct {
	sync.RWMutex
	pending map[dataReference]uint
}

// dataWrite keeps blobs reachable between storage preparation and index commit.
// It belongs to one document write, then to its batch if the write is staged.
type dataWrite struct {
	store      *dataStore
	references map[dataReference]struct{}
}

func (ds *dataStore) newWrite() *dataWrite {
	return &dataWrite{store: ds, references: make(map[dataReference]struct{})}
}

// retainLocked registers a reference while its shard is locked. Registering a
// newly written blob before unlocking leaves no gap for orphan cleanup.
func (w *dataWrite) retainLocked(shard *dataShard, ref dataReference) {
	if _, exists := w.references[ref]; exists {
		return
	}
	if shard.pending == nil {
		shard.pending = make(map[dataReference]uint)
	}
	shard.pending[ref]++
	w.references[ref] = struct{}{}
}

func (w *dataWrite) retain(subdir, key string) {
	if key == "" {
		return
	}
	shard := w.store.shard(key)
	shard.Lock()
	defer shard.Unlock()
	w.retainLocked(shard, dataReference{subdir: subdir, key: key})
}

// release makes uncommitted blobs eligible for cleanup again. Committed blobs
// remain protected by their index references. It is safe to release twice.
func (w *dataWrite) release() {
	if w == nil {
		return
	}
	for ref := range w.references {
		shard := w.store.shard(ref.key)
		shard.Lock()
		if shard.pending[ref] <= 1 {
			delete(shard.pending, ref)
		} else {
			shard.pending[ref]--
		}
		shard.Unlock()
	}
	clear(w.references)
}

func newDataStore(dir string) *dataStore {
	return &dataStore{dir: dir}
}

// shard returns the mutex responsible for key, using the first byte of the
// hex-encoded SHA-256 string as the shard index (256 shards total).
func (ds *dataStore) shard(key string) *dataShard {
	if len(key) < 2 {
		return &ds.shards[0]
	}
	hi := hexNibble(key[0])
	lo := hexNibble(key[1])
	return &ds.shards[hi<<4|lo]
}

// hexNibble converts a lowercase hex character to its numeric value.
func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return 0
	}
}

// write stores and retains a blob until this document write is released.
func (w *dataWrite) write(subdir string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(data)
	key := fmt.Sprintf("%x", sum)
	mu := w.store.shard(key)
	mu.Lock()
	defer mu.Unlock()
	if err := writeFileLocked(w.store.dir, subdir, key, data, w.store.compression); err != nil {
		return "", err
	}
	w.retainLocked(mu, dataReference{subdir: subdir, key: key})
	return key, nil
}

// read decompresses and returns the data file identified by key. Acquires a
// shared lock on the key's shard, allowing concurrent reads of the same file.
func (ds *dataStore) read(subdir, key string) ([]byte, error) {
	mu := ds.shard(key)
	mu.RLock()
	defer mu.RUnlock()
	return readFileLocked(ds.dir, subdir, key)
}

// deleteIfOrphaned acquires the exclusive shard lock for key, calls refCount
// to check whether any indexed document still references it, and removes the
// file only when neither pending writes nor committed documents need it.
func (ds *dataStore) deleteIfOrphaned(field, subdir, key string, refCount func(string, string) uint64) {
	path, err := dataFilePath(ds.dir, subdir, key)
	if err != nil {
		log.Warn().Err(err).Str("key", key).Str("subdir", subdir).Msg("ignored invalid stored data reference")
		return
	}
	mu := ds.shard(key)
	mu.Lock()
	defer mu.Unlock()
	if mu.pending[dataReference{subdir: subdir, key: key}] == 0 && refCount(field, key) == 0 {
		for _, candidate := range blobVariants(path) {
			if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
				log.Warn().Err(err).Str("path", candidate).Msg("failed to remove orphaned data file")
			}
		}
	}
}

// cleanup removes unreferenced compressed files under subdir. For each candidate file
// the per-key shard lock protects both pending references and the live
// refCount query, keeping the check and removal atomic with storage writes.
func (ds *dataStore) cleanup(field, subdir string, refCount func(string, string) uint64) (int, error) {
	root := filepath.Join(ds.dir, subdir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}
	removed := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("error accessing data file during cleanup")
			return nil
		}
		extension := filepath.Ext(path)
		if info.IsDir() || (extension != gzipExtension && extension != zstdExtension) {
			return nil
		}
		// Reconstruct the key from the 3-level directory prefix + filename.
		// rel is like "aa/bb/cc/ddeeff....gz" or "aa/bb/cc/ddeeff....zst"
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		parts := splitPath(rel)
		if len(parts) != 4 {
			return nil
		}
		// Strip the codec extension to recover the hash tail.
		key := parts[0] + parts[1] + parts[2] + strings.TrimSuffix(parts[3], extension)
		if !validDataKey(key) {
			return nil
		}
		mu := ds.shard(key)
		mu.Lock()
		if mu.pending[dataReference{subdir: subdir, key: key}] == 0 && refCount(field, key) == 0 {
			if rerr := os.Remove(path); rerr != nil {
				log.Warn().Err(rerr).Str("path", path).Msg("failed to remove orphaned data file")
			} else {
				removed++
				log.Debug().Str("key", key).Str("subdir", subdir).Msg("removed orphaned data file")
			}
		}
		mu.Unlock()
		return nil
	})
	return removed, err
}

func validDataKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// dataFilePath validates the key before deriving a filesystem path. Stored
// references may have been imported or saved before submission validation.
// Layout: {dataDir}/{subdir}/{key[0:2]}/{key[2:4]}/{key[4:6]}/{key[6:]}.gz
func dataFilePath(dataDir, subdir, key string) (string, error) {
	if !validDataKey(key) {
		return "", errors.New("invalid data key")
	}
	return filepath.Join(dataDir, subdir, key[0:2], key[2:4], key[4:6], key[6:]+gzipExtension), nil
}

// blobVariants returns the legacy gzip path followed by the Zstandard path.
func blobVariants(gzipPath string) [2]string {
	return [2]string{gzipPath, strings.TrimSuffix(gzipPath, gzipExtension) + zstdExtension}
}

// writeFileLocked publishes a complete compressed blob. Existing blobs are
// reused by content hash, including blobs stored with the other codec.
func writeFileLocked(dataDir, subdir, key string, data []byte, compression string) error {
	path, err := dataFilePath(dataDir, subdir, key)
	if err != nil {
		return err
	}
	variants := blobVariants(path)
	for _, candidate := range variants {
		if _, err := os.Stat(candidate); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check stored data file: %w", err)
		}
	}
	if compression == config.CompressionZstd {
		path = variants[1]
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return fmt.Errorf("create data file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	var writer io.WriteCloser
	if compression == config.CompressionZstd {
		writer, err = zstd.NewWriter(file, zstd.WithEncoderConcurrency(1))
		if err != nil {
			return fmt.Errorf("create zstd writer: %w", err)
		}
	} else {
		writer = gzip.NewWriter(file)
	}
	_, writeErr := writer.Write(data)
	if err := errors.Join(writeErr, writer.Close(), file.Close()); err != nil {
		return fmt.Errorf("write compressed data: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish compressed data: %w", err)
	}
	return nil
}

// readFileLocked decompresses data from either format. The caller holds the
// relevant shard lock.
func readFileLocked(dataDir, subdir, key string) ([]byte, error) {
	path, err := dataFilePath(dataDir, subdir, key)
	if err != nil {
		return nil, err
	}
	useZstd := false
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		useZstd = true
		file, err = os.Open(blobVariants(path)[1])
	}
	if err != nil {
		return nil, fmt.Errorf("open data file: %w", err)
	}
	defer file.Close()
	if useZstd {
		reader, err := zstd.NewReader(file, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("create zstd reader: %w", err)
		}
		defer reader.Close()
		return io.ReadAll(reader)
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// splitPath splits a filepath into its individual components.
func splitPath(p string) []string {
	var parts []string
	for {
		dir, file := filepath.Split(filepath.Clean(p))
		if file == "" || file == "." {
			break
		}
		parts = append([]string{file}, parts...)
		p = dir
		if dir == "" || dir == "." {
			break
		}
	}
	return parts
}
