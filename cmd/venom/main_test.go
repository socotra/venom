//go:build testbincover

package main

import (
	"testing"

	"github.com/confluentinc/bincover"
)

func TestBincoverRunMain(t *testing.T) {
	bincover.RunTest(main)
}
