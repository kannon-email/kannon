package container

import (
	"context"
	"sync"
)

type hzFn func(ctx context.Context) error

type HZ struct {
	name string
	hz   hzFn
}

func (c *Container) addHZ(name string, hz hzFn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hzs = append(c.hzs, HZ{name: name, hz: hz})
}

type HZRes map[string]error

func (c *Container) HZ(ctx context.Context) HZRes {
	c.mu.Lock()
	hzs := append([]HZ(nil), c.hzs...)
	c.mu.Unlock()

	hzResult := make(HZRes)

	mu := sync.Mutex{}

	wg := sync.WaitGroup{}
	wg.Add(len(hzs))

	for _, hz := range hzs {
		go func(hz HZ) {
			defer wg.Done()
			if err := hz.hz(ctx); err != nil {
				mu.Lock()
				hzResult[hz.name] = err
				mu.Unlock()
			}
		}(hz)
	}

	wg.Wait()

	return hzResult
}
