package dumper

import (
	"container/list"
	"fmt"
	"hlsdump/pkg/logger"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

var endTask = &Task{}

type Task struct {
	url      string
	filename string
}

type TaskManager struct {
	mutex  *sync.Mutex
	client *http.Client
	queue  *list.List
	wg     *sync.WaitGroup
	retry  int
}

func NewTaskManager(retry, timeout int) *TaskManager {
	tr := &http.Transport{
		DisableKeepAlives:   false,
		MaxConnsPerHost:     5,
		MaxIdleConnsPerHost: 5,
		Proxy:               http.ProxyFromEnvironment,
	}
	c := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(timeout) * time.Second,
	}

	return &TaskManager{
		retry:  retry,
		client: c,
		mutex:  &sync.Mutex{},
		queue:  list.New(),
		wg:     &sync.WaitGroup{},
	}
}

func (tm *TaskManager) Push(t *Task) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tm.queue.PushBack(t)
	return nil
}

func (tm *TaskManager) Run() {
	log := logger.Inst()
	for i := 0; i < 5; i++ {
		tm.wg.Add(1)
		go func() {
			defer tm.wg.Done()
			for {
				task := func() *Task {
					tm.mutex.Lock()
					defer tm.mutex.Unlock()
					if tm.queue.Len() == 0 {
						return nil
					}
					front := tm.queue.Front()
					task := front.Value.(*Task)
					tm.queue.Remove(front)
					return task
				}()

				if task == nil {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if task == endTask {
					break
				}
				for j := 0; j < tm.retry; j++ {
					err := tm.downloadSegment(task.url, task.filename)
					if err != nil {
						log.Error(fmt.Sprintf("download segment:%s failed at %d time", task.url, j+1), zap.Error(err))
					} else {
						log.Info("download segment done", zap.String("url", task.url), zap.String("file", task.filename))
						break
					}
				}
			}
		}()
	}
}

func (tm *TaskManager) Stop() {
	for i := 0; i < 5; i++ {
		tm.Push(endTask)
	}
	tm.wg.Wait()
}

func (tm *TaskManager) downloadSegment(uri string, filename string) error {
	resp, err := tm.client.Get(uri)
	if err != nil {
		return fmt.Errorf("HttpGetFailed:%v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HttpStatusNotOK:%v", resp.Status)
	}

	tsFile, err := os.OpenFile(filename, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("OpenTsFileFailed:%v", err)
	}
	_, err = io.Copy(tsFile, resp.Body)
	if err != nil {
		return fmt.Errorf("WriteTsFileFailed:%v", err)
	}
	err = tsFile.Sync()
	if err != nil {
		return fmt.Errorf("SyncTsFileFailed:%v", err)
	}
	err = tsFile.Close()
	if err != nil {
		return fmt.Errorf("CloseTsFileFailed:%v", err)
	}
	return nil
}
