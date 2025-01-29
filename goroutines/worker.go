package goroutines

import (
    "fmt"
    "log"
    "sync"
    "time"
)

type Task struct {
    ID       string
    Action   func() error
    StartTime time.Time
}

type Worker struct {
    id       int
    taskChan chan Task
    quit     chan struct{}
}

type WorkerPool struct {
    workers  []*Worker
    taskChan chan Task
    wg       sync.WaitGroup
    errors   chan error
}

func NewWorkerPool(numWorkers int) *WorkerPool {
    pool := &WorkerPool{
        workers:  make([]*Worker, numWorkers),
        taskChan: make(chan Task, 100),
        errors:   make(chan error, 100),
    }

    for i := 0; i < numWorkers; i++ {
        worker := &Worker{
            id:       i,
            taskChan: pool.taskChan,
            quit:     make(chan struct{}),
        }
        pool.workers[i] = worker
        pool.wg.Add(1)
        go worker.start(&pool.wg)
    }

    go pool.errorHandler()
    return pool
}

func (w *Worker) start(wg *sync.WaitGroup) {
    defer wg.Done()
    log.Printf("Worker %d: started", w.id)

    for {
        select {
        case task := <-w.taskChan:
            log.Printf("Worker %d: starting task %s", w.id, task.ID)
            if err := task.Action(); err != nil {
                log.Printf("Worker %d: task execution error %s: %v", w.id, task.ID, err)
            }
            duration := time.Since(task.StartTime)
            log.Printf("Worker %d: task %s completed in %v", w.id, task.ID, duration)

        case <-w.quit:
            log.Printf("Worker %d: shutting down", w.id)
            return
        }
    }
}

func (p *WorkerPool) ExecuteAsync(action func() error) error {
    task := Task{
        ID:        fmt.Sprintf("task-%d", time.Now().UnixNano()),
        Action:    action,
        StartTime: time.Now(),
    }

    select {
    case p.taskChan <- task:
        return nil
    case <-time.After(time.Second):
        return fmt.Errorf("timeout adding task %s to queue", task.ID)
    }
}

func (p *WorkerPool) errorHandler() {
    for err := range p.errors {
        log.Printf("Pool error: %v", err)
    }
}

func (p *WorkerPool) Shutdown() {
    log.Println("Worker pool: starting shutdown")

    for _, worker := range p.workers {
        close(worker.quit)
    }
    p.wg.Wait()
    close(p.taskChan)
    close(p.errors)
    log.Println("Worker pool: shutdown complete")
}
