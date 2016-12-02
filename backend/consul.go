package main

import (
	log "github.com/Sirupsen/logrus"
	consulApi "github.com/hashicorp/consul/api"
)

type ConsulClient struct {
	client *consulApi.Client
}

var consulClient *ConsulClient = new(ConsulClient)

// newConsulClient uses http protocol
func NewConsulClient(url string) (*consulApi.Client, error) {
	/* url: "127.0.0.1:8500",*/
	cli, err := consulApi.NewClient(&consulApi.Config{
		Address: url,
	})
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return cli, nil
}
