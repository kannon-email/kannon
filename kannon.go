package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ludusrusso/kannon/pkg/api"
	"github.com/ludusrusso/kannon/pkg/dispatcher"
	"github.com/ludusrusso/kannon/pkg/sender"
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	if sv := viper.Sub("sender"); sv != nil {
		wg.Add(1)
		sender.Run(ctx, sv)
		wg.Done()
	}

	if sv := viper.Sub("dispatcher"); sv != nil {
		wg.Add(1)
		dispatcher.Run(ctx, sv)
		wg.Done()
	}

	if sv := viper.Sub("api"); sv != nil {
		wg.Add(1)
		api.Run(ctx, sv)
		wg.Done()
	}

	wg.Wait()
}
