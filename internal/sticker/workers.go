package sticker

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/panjf2000/ants/v2"
)

var (
	downloadPool *ants.PoolWithFunc
	poolOnce     sync.Once
)

// DownloadTask holds the parameters for a parallel sticker download.
type DownloadTask struct {
	Wg   sync.WaitGroup
	Fn   func() error
	Err  error
}

func downloadWorker(i any) {
	task := i.(*DownloadTask)
	defer task.Wg.Done()
	task.Err = task.Fn()
}

// InitWorkers creates the ants worker pools used for parallel downloads.
func InitWorkers() error {
	var initErr error
	poolOnce.Do(func() {
		var err error
		downloadPool, err = ants.NewPoolWithFunc(8, downloadWorker)
		if err != nil {
			initErr = fmt.Errorf("failed to create download pool: %w", err)
			return
		}
		slog.Info("sticker worker pools initialized", "downloadPoolSize", 8)
	})
	return initErr
}

// CloseWorkers releases all ants worker pools.
func CloseWorkers() {
	if downloadPool != nil {
		downloadPool.Release()
		slog.Info("sticker worker pools released")
	}
}

// SubmitDownload submits a download task to the pool.
// The caller must call task.Wg.Wait() to wait for completion, then check task.Err.
func SubmitDownload(task *DownloadTask) error {
	if downloadPool == nil {
		return fmt.Errorf("download pool not initialized, call InitWorkers first")
	}
	task.Wg.Add(1)
	return downloadPool.Invoke(task)
}
