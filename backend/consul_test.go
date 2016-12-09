package main

import (
	"log"
	"testing"
)

func TestConsulUnit(t *testing.T) {
	t.Skip()
	c, err := NewConsulClient("10.9.46.73:8500")
	if err != nil {
		log.Printf("get consul client error: %v", err)
	}

	servs, _, err := c.Health().Service("doc-ui-service", "NOMAD_ALLOC_ID=70686dbb-62fa-0da0-8868-3361a6b329f7", true, nil)
	if err != nil {
		log.Printf("err get service: %v", err)
	}
	for _, serv := range servs {
		log.Printf("service: %v", serv.Service.Address)
	}

}
