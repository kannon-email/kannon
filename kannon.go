package main

import (
	"github.com/ludusrusso/kannon/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := cmd.Execute(); err != nil {
		logrus.Fatalf("Error: %v", err)
	}
}
