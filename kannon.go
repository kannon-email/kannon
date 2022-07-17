package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/ludusrusso/kannon/pkg/api"
	"github.com/ludusrusso/kannon/pkg/bounce"
	"github.com/ludusrusso/kannon/pkg/dispatcher"
	"github.com/ludusrusso/kannon/pkg/sender"
	"github.com/ludusrusso/kannon/pkg/stats"
	"github.com/spf13/viper"
)

func init() {
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/kannon/")
	viper.AddConfigPath("$HOME/.kannon")
	viper.AddConfigPath(".")
}

func main() {
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

	ctx := context.Background()

	var wg sync.WaitGroup

	if sv := viper.Sub("sender"); sv != nil {
		wg.Add(1)
		go func() {
			sender.Run(ctx, sv)
			wg.Done()
		}()
	}

	if sv := viper.Sub("dispatcher"); sv != nil {
		wg.Add(1)
		go func() {
			dispatcher.Run(ctx, sv)
			wg.Done()
		}()
	}

	if sv := viper.Sub("api"); sv != nil {
		wg.Add(1)
		go func() {
			api.Run(ctx, sv)
			wg.Done()
		}()
	}

	if sv := viper.Sub("stats"); sv != nil {
		wg.Add(1)
		go func() {
			stats.Run(ctx, sv)
			wg.Done()
		}()
	}

	if sv := viper.Sub("bounce"); sv != nil {
		wg.Add(1)
		go func() {
			bounce.Run(ctx, sv)
			wg.Done()
		}()
	}

	wg.Wait()
}
