// ЗАДАНИЕ:
// * сделать из плохого кода хороший;
// * важно сохранить логику появления ошибочных тасков;
// * сделать правильную мультипоточность обработки заданий.
// Обновленный код отправить через merge-request.

// приложение эмулирует получение и обработку тасков, пытается и получать и обрабатывать в многопоточном режиме
// В конце должно выводить успешные таски и ошибки выполнены остальных тасков
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	numTasks        = 1000
	numWorkers      = 5
	bufferSize      = 100
	processingDelay = 150 * time.Millisecond
	maxProcessTime  = 30 * time.Second
)

type Task struct {
	ID        int
	CreatedAt time.Time
	DoneAt    time.Time
	Result    string
}

type TaskError struct {
	TaskID      int
	Time        time.Time
	Message     string
	Description string
}

func (e TaskError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("Task ID %d at %s: %s (%s)", 
			e.TaskID, e.Time.Format(time.RFC3339), e.Message, e.Description)
	}
	return fmt.Sprintf("Task ID %d at %s: %s", 
		e.TaskID, e.Time.Format(time.RFC3339), e.Message)
}

type TaskGenerator struct {
	out chan<- Task
}

func NewTaskGenerator(out chan<- Task) *TaskGenerator {
	return &TaskGenerator{out: out}
}

func (g *TaskGenerator) Generate(ctx context.Context) error {
	defer close(g.out)

	for i := 0; i < numTasks; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			task := Task{
				ID:        i,
				CreatedAt: time.Now(),
			}

			switch time.Now().Nanosecond() % 3 {
			case 0:
			case 1:
				task.CreatedAt = time.Time{}
			case 2:
				if i > numTasks/2 {
					return fmt.Errorf("critical generator error at task %d", i)
				}
			}

			g.out <- task
		}
	}
	return nil
}

type TaskProcessor struct {
	id         int
	in         <-chan Task
	successful chan<- Task
	failed     chan<- error
}

func NewTaskProcessor(id int, in <-chan Task, successful chan<- Task, failed chan<- error) *TaskProcessor {
	return &TaskProcessor{
		id:         id,
		in:         in,
		successful: successful,
		failed:     failed,
	}
}

func (p *TaskProcessor) Process(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case task, ok := <-p.in:
			if !ok {
				return nil
			}
			if err := p.processTask(task); err != nil {
				if isCriticalError(err) {
					return fmt.Errorf("critical error in processor %d: %w", p.id, err)
				}
				p.failed <- err
			}
		}
	}
}

func (p *TaskProcessor) processTask(task Task) error {
	time.Sleep(processingDelay)

	task.DoneAt = time.Now()

	if task.CreatedAt.IsZero() {
		return &TaskError{
			TaskID:      task.ID,
			Time:        task.DoneAt,
			Message:     "processing error",
			Description: "invalid creation time",
		}
	}

	if task.ID%100 == 99 {
		return &TaskError{
			TaskID:      task.ID,
			Time:        task.DoneAt,
			Message:     "critical error",
			Description: "processor failure simulation",
		}
	}

	task.Result = "task completed successfully"
	p.successful <- task
	return nil
}

func isCriticalError(err error) bool {
	if taskErr, ok := err.(*TaskError); ok {
		return taskErr.Message == "critical error"
	}
	return false
}

type ResultCollector struct {
	successful <-chan Task
	failed     <-chan error
	wg         *sync.WaitGroup
	stats      *ProcessingStats
}

type ProcessingStats struct {
	mu            sync.Mutex
	successCount  int
	failureCount  int
	startTime     time.Time
	criticalError error
}

func NewResultCollector(successful <-chan Task, failed <-chan error, wg *sync.WaitGroup) *ResultCollector {
	return &ResultCollector{
		successful: successful,
		failed:     failed,
		wg:         wg,
		stats: &ProcessingStats{
			startTime: time.Now(),
		},
	}
}

func (c *ResultCollector) Collect(ctx context.Context) {
	defer c.wg.Done()

	for c.successful != nil || c.failed != nil {
		select {
		case <-ctx.Done():
			log.Printf("Collection interrupted: %v", ctx.Err())
			return

		case task, ok := <-c.successful:
			if !ok {
				c.successful = nil
				continue
			}
			c.stats.mu.Lock()
			c.stats.successCount++
			c.stats.mu.Unlock()
			
			log.Printf("Successful task ID %d: processed in %v", 
				task.ID, task.DoneAt.Sub(task.CreatedAt))

		case err, ok := <-c.failed:
			if !ok {
				c.failed = nil
				continue
			}
			c.stats.mu.Lock()
			c.stats.failureCount++
			if isCriticalError(err) {
				c.stats.criticalError = err
			}
			c.stats.mu.Unlock()
			
			log.Printf("Failed task: %v", err)
		}
	}

	duration := time.Since(c.stats.startTime)
	log.Printf("Processing completed in %v. Successful: %d, Failed: %d", 
		duration, c.stats.successCount, c.stats.failureCount)
	
	if c.stats.criticalError != nil {
		log.Printf("Critical error occurred: %v", c.stats.criticalError)
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), maxProcessTime)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, starting graceful shutdown", sig)
		cancel()
	}()

	tasks := make(chan Task, bufferSize)
	successful := make(chan Task, bufferSize)
	failed := make(chan error, bufferSize)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		generator := NewTaskGenerator(tasks)
		if err := generator.Generate(ctx); err != nil {
			log.Printf("Generator error: %v", err)
			cancel()
		}
	}()

	var workerWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func(id int) {
			defer workerWg.Done()
			processor := NewTaskProcessor(id, tasks, successful, failed)
			if err := processor.Process(ctx); err != nil {
				log.Printf("Processor %d error: %v", id, err)
				if isCriticalError(err) {
					cancel()
				}
			}
		}(i)
	}

	go func() {
		workerWg.Wait()
		close(successful)
		close(failed)
	}()

	wg.Add(1)
	collector := NewResultCollector(successful, failed, &wg)
	go collector.Collect(ctx)

	wg.Wait()
}
