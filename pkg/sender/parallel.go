package sender

import "sync"

type Parallel struct {
	wg sync.WaitGroup
	ch chan bool
}

func NewParallel(maxJobs uint) *Parallel {
	return &Parallel{
		wg: sync.WaitGroup{},
		ch: make(chan bool, maxJobs),
	}
}

func (p *Parallel) RunTask(fn func()) {
	p.wg.Add(1)
	p.ch <- true
	go func() {
		defer p.wg.Done()
		defer func() { <-p.ch }()
		fn()
	}()
}

func (p *Parallel) WaitAndClose() {
	p.wg.Wait()
	close(p.ch)
}
