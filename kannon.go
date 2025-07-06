package main

import (
	"github.com/kannon-email/kannon/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := cmd.Execute(); err != nil {
		logrus.Fatalf("Error: %v", err)
	}
}
