package main

import (
	"fmt"
	"sync"
	"time"
)

// ЗАДАНИЕ:
// * сделать из плохого кода хороший;
// * важно сохранить логику появления ошибочных тасков;
// * сделать правильную мультипоточность обработки заданий.
// Обновленный код отправить через merge-request.

// приложение эмулирует получение и обработку тасков, пытается и получать и обрабатывать в многопоточном режиме
// В конце должно выводить успешные таски и ошибки выполнены остальных тасков

// A Ttype represents a meaninglessness of our life
type Ttype struct {
	id         int
	cT         string // время создания
	fT         string // время выполнения
	taskRESULT []byte
}

func main() {
	var wg sync.WaitGroup
	taskCreturer := func(tasks chan<- Ttype) {
		defer wg.Done()
		for i := 0; i < 1000; i++ { // 1000 тасков для примера
			ft := time.Now().Format(time.RFC3339)
			if time.Now().Nanosecond()%2 > 0 { // вот такое условие появления ошибочных тасков
				ft = "Some error occured"
			}
			a <- Ttype{cT: ft, id: i} // передаем таск на выполнение
		}
		close(tasks)
	}

	superChan := make(chan Ttype, 10)
	wg.Add(1)
	go func() {
		defer wg.Done()
		taskCreturer(superChan)
	}()

	task_worker := func(tasks <- Ttype, doneTasks chan<- Ttype, undoneTasks<- error) {
		defer wg.Done()
		for t := range tasks {
			_, err : time.Parse(time.RFC3339, t.cT)
			if err == nil {
				t.taskRESULT = []byte("task has been successed")
			} else {
				t.taskRESULT = []byte("something went wrong")
			}
			t.fT = time.Now().Format(time.RFC3339Nano)
			time.Sleep(time.Millisecond * 150) // симуляция работы

			if string(t.taskRESULT) == "task has been successed" {
				doneTasks <- t
			} else {
				undoneTasks <- fmt.Errorf("Task id %d time %s, error %s", t.id, t.cT, t.taskRESULT)
			}
		}
	}

	doneTasks := make(chan Ttype, 10)
	undoneTasks := make(chan error, 10)

	wg.Add(1)
	go func() {
		defer wg.Done()
		task_worker(superChan, doneTasks, undoneTasks)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for doneTasks != nil || undoneTasks != nil {
			select {
			case task, ok := <- doneTasks:
				if !ok {
					doneTasks = nil
				} else {
					fmt.Printf("Done task: %v\n", task)
				}
			case err, ok := <-undoneTasks:
				if !ok {
					undoneTasks = nil
					
				} else {
					fmt.Printf("Failed task: %v\n", err)
				}
			}
		}
	}()
	wg.Wait()
}
