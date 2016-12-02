package main

import (
	"fmt"
	"testing"
)

func TestNomadUnit(t *testing.T) {
	t.Skip()
	broadcast := make(chan *Action)
	nomad := NewNomad(fmt.Sprintf("http://%s:%s", "10.9.46.73", "4646"), broadcast)
	nomad.watchAllocs()

}
