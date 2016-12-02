package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/fatih/set.v0"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/command"
)

const (
	// bytesToLines is an estimation of how many bytes are in each log line.
	// This is used to set the offset to read from when a user specifies how
	// many lines to tail from.
	bytesToLines int64 = 120

	// defaultTailLines is the number of lines to tail by default.
	defaultTailLines int64 = 250

	// If a file exceeds an estimate of 250 loglines we start tailing it
	// from the end, otherwise the whole file is retrieved and followed.
	maxFileSize int64 = defaultTailLines * bytesToLines
)

// Connection monitors the websocket connection. It processes any action
// received on the websocket and sends out actions on Nomad state changes. It
// maintains a set to keep track of the running watches.
type Connection struct {
	socket *websocket.Conn

	hub *Hub

	receive chan *Action
	send    chan *Action

	destroyCh chan struct{}

	watches *set.Set
}

type allocWithContainerIP struct {
	ID                 string
	EvalID             string
	Name               string
	NodeID             string
	NodeIP             string
	JobID              string
	ContainerIP        string
	Job                *api.Job
	TaskGroup          string
	Resources          *api.Resources
	TaskResources      map[string]*api.Resources
	Services           map[string]string
	Metrics            *api.AllocationMetric
	DesiredStatus      string
	DesiredDescription string
	ClientStatus       string
	ClientDescription  string
	TaskStates         map[string]*api.TaskState
	CreateIndex        uint64
	ModifyIndex        uint64
	CreateTime         int64
}

type jobWithNomadAddrAndJenkinsURL struct {
	Region            string
	ID                string
	Name              string
	Type              string
	Priority          int
	AllAtOnce         bool
	Datacenters       []string
	Constraints       []*api.Constraint
	TaskGroups        []*api.TaskGroup
	Update            *api.UpdateStrategy
	Periodic          *api.PeriodicConfig
	Meta              map[string]string
	Status            string
	StatusDescription string
	CreateIndex       uint64
	ModifyIndex       uint64
	NomadAddr         string
	JenkinsURL        string
}

// NewConnection creates a new connection.
func NewConnection(hub *Hub, socket *websocket.Conn) *Connection {
	return &Connection{
		watches:   set.New(),
		hub:       hub,
		socket:    socket,
		receive:   make(chan *Action),
		send:      make(chan *Action),
		destroyCh: make(chan struct{}),
	}
}

func (c *Connection) writePump() {
	defer func() {
		c.socket.Close()
	}()

	for {
		action, ok := <-c.send
		if !ok {
			if err := c.socket.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
				logger.Errorf("Could not write close message to websocket: %s", err)
			}
			return
		}
		if err := c.socket.WriteJSON(action); err != nil {
			logger.Errorf("Could not write action to websocket: %s", err)
		}
	}
}

func (c *Connection) readPump() {
	defer func() {
		c.watches.Clear()
		c.hub.unregister <- c
		c.socket.Close()
	}()

	// Register this connection with the hub for broadcast updates
	c.hub.register <- c

	// Flush all current state to the websocket
	c.hub.nomad.FlushAll(c)

	var action Action
	for {
		err := c.socket.ReadJSON(&action)
		if err != nil {
			break
		}
		c.process(action)
	}
}

func (c *Connection) process(action Action) {
	switch action.Type {
	case fetchMember:
		c.fetchMember(action)
	case fetchNode:
		c.fetchNode(action)
	case fetchDir:
		c.fetchDir(action)
	case watchJob:
		go c.watchJob(action)
	case watchAlloc:
		go c.watchAlloc(action)
	case watchEval:
		go c.watchEval(action)
	case watchMember:
		go c.watchMember(action)
	case watchNode:
		go c.watchNode(action)
	case watchFile:
		go c.watchFile(action)
	case unwatchEval:
		fallthrough
	case unwatchMember:
		fallthrough
	case unwatchNode:
		fallthrough
	case unwatchJob:
		fallthrough
	case unwatchAlloc:
		c.watches.Remove(action.Payload.(string))
	case unwatchFile:
		c.watches.Remove(action.Payload.(string))
	}
}

// Handle monitors the websocket connection for incoming actions. It sends
// out actions on state changes.
func (c *Connection) Handle() {
	go c.writePump()
	c.readPump()

	// Kill any remaining watcher routines
	close(c.destroyCh)
}

func (c *Connection) watchAlloc(action Action) {
	allocID := action.Payload.(string)
	defer func() {
		c.watches.Remove(allocID)
		logger.Infof("Stopped watching alloc with id: %s", allocID)
	}()
	c.watches.Add(allocID)

	q := &api.QueryOptions{WaitIndex: 1}
	queryOptionsForNode := &api.QueryOptions{WaitIndex: 1}
	for {
		select {
		case <-c.destroyCh:
			return
		default:
			alloc, meta, err := c.hub.nomad.Client.Allocations().Info(allocID, q)
			if err != nil {
				logger.Errorf("connection: unable to fetch alloc info: %s", err)
				time.Sleep(10 * time.Second)
				continue
			}

			consulAddr := os.Getenv("CONSUL_ADDR")
			consulClient, err := NewConsulClient(consulAddr)
			if err != nil {
				log.Printf("get consul client error: %v", err)
			}

			services, _, err := consulClient.Catalog().Services(nil)
			if err != nil {
				log.Printf("Cannot get services on consul err: %v", err)
			}

			alloInfo, _, err := c.hub.nomad.Client.Allocations().Info(allocID, nil)
			log.Printf("allo id: %v", alloInfo.ID)
			if err != nil {
				log.Printf("Get allocation info err: %v", err)
			}

			containerIP := ""
			for service := range services {
				for _, tag := range services[service] {
					if tag == "NOMAD-ALLOC-ID--"+allocID {
						servs, _, err := consulClient.Health().Service(service, "NOMAD-ALLOC-ID--"+allocID, true, nil)
						if err != nil {
							log.Printf("err : %v", err)
						}
						for id, serv := range servs {
							containerIP = serv.Service.Address
							log.Printf("%v, contiv ip: %v\n", id, containerIP)
						}
					}
				}
			}

			// get allocation's node IP
			node, metaForNode, err := c.hub.nomad.Client.Nodes().Info(alloc.NodeID, queryOptionsForNode)
			var nodeIP string

			if err != nil {
				logger.Errorf("connection: unable to fetch info of node [%s]: %s", alloc.NodeID, err.Error())
			} else {
				// get rid of the last ":", which is the port number.
				lastColon := strings.LastIndex(node.HTTPAddr, ":")
				if lastColon == -1 {
					nodeIP = node.HTTPAddr
				} else {
					nodeIP = node.HTTPAddr[:lastColon]
				}
			}

			if !c.watches.Has(allocID) {
				return
			}
			resultPayload := allocWithContainerIP{
				ID:                 alloc.ID,
				EvalID:             alloc.EvalID,
				Name:               alloc.Name,
				NodeID:             alloc.NodeID,
				NodeIP:             nodeIP,
				JobID:              alloc.JobID,
				ContainerIP:        containerIP,
				Job:                alloc.Job,
				TaskGroup:          alloc.TaskGroup,
				Resources:          alloc.Resources,
				TaskResources:      alloc.TaskResources,
				Services:           alloc.Services,
				Metrics:            alloc.Metrics,
				DesiredStatus:      alloc.DesiredStatus,
				DesiredDescription: alloc.DesiredDescription,
				ClientStatus:       alloc.ClientStatus,
				ClientDescription:  alloc.ClientDescription,
				TaskStates:         alloc.TaskStates,
				CreateIndex:        alloc.CreateIndex,
				ModifyIndex:        alloc.ModifyIndex,
				CreateTime:         alloc.CreateTime,
			}
			c.send <- &Action{Type: fetchedAlloc, Payload: resultPayload}

			w := metaForNode.LastIndex
			if queryOptionsForNode.WaitIndex > metaForNode.LastIndex {
				w = queryOptionsForNode.WaitIndex
			}
			queryOptionsForNode = &api.QueryOptions{WaitIndex: w}

			// set wait index for node queries:
			waitIndex := meta.LastIndex
			if q.WaitIndex > meta.LastIndex {
				waitIndex = q.WaitIndex
			}
			q = &api.QueryOptions{WaitIndex: waitIndex, WaitTime: 10 * time.Second}
		}
	}
}

func (c *Connection) watchEval(action Action) {
	evalID := action.Payload.(string)

	defer func() {
		c.watches.Remove(evalID)
		logger.Infof("Stopped watching eval with id: %s", evalID)
	}()
	c.watches.Add(evalID)

	logger.Infof("Started watching eval with id: %s", evalID)

	q := &api.QueryOptions{WaitIndex: 1}
	for {
		select {
		case <-c.destroyCh:
			return
		default:
			eval, meta, err := c.hub.nomad.Client.Evaluations().Info(evalID, q)
			if err != nil {
				logger.Errorf("connection: unable to fetch eval info: %s", err)
				time.Sleep(10 * time.Second)
				continue
			}
			if !c.watches.Has(evalID) {
				return
			}
			c.send <- &Action{Type: fetchedEval, Payload: eval}

			waitIndex := meta.LastIndex
			if q.WaitIndex > meta.LastIndex {
				waitIndex = q.WaitIndex
			}
			q = &api.QueryOptions{WaitIndex: waitIndex, WaitTime: 10 * time.Second}
		}
	}
}

func (c *Connection) fetchMember(action Action) {
	memberID := action.Payload.(string)
	member, err := c.hub.nomad.MemberWithID(memberID)
	if err != nil {
		logger.Errorf("websocket: unable to fetch member %q: %s", memberID, err)
		return
	}

	c.send <- &Action{Type: fetchedMember, Payload: member}
}

func (c *Connection) watchMember(action Action) {
	memberID := action.Payload.(string)

	defer func() {
		c.watches.Remove(memberID)
		logger.Infof("Stopped watching member with id: %s", memberID)
	}()
	c.watches.Add(memberID)

	logger.Infof("Started watching member with id: %s", memberID)

	for {
		select {
		case <-c.destroyCh:
			return
		default:
			member, err := c.hub.nomad.MemberWithID(memberID)
			if err != nil {
				logger.Errorf("connection: unable to fetch member info: %s", err)
				time.Sleep(10 * time.Second)
				continue
			}
			if !c.watches.Has(memberID) {
				return
			}
			c.send <- &Action{Type: fetchedMember, Payload: member}

			time.Sleep(10 * time.Second)
		}
	}
}

func (c *Connection) fetchNode(action Action) {
	nodeID := action.Payload.(string)
	node, _, err := c.hub.nomad.Client.Nodes().Info(nodeID, nil)
	if err != nil {
		logger.Errorf("websocket: unable to fetch node %q: %s", nodeID, err)
	}
	c.send <- &Action{Type: fetchedNode, Payload: node}
}

func (c *Connection) watchNode(action Action) {
	log.Printf("type is : %v", action.Payload.(string))
	nodeID := action.Payload.(string)
	defer func() {
		c.watches.Remove(nodeID)
		logger.Infof("Stopped watching node with id: %s", nodeID)
	}()
	c.watches.Add(nodeID)

	logger.Infof("Started watching node with id: %s", nodeID)

	q := &api.QueryOptions{WaitIndex: 1}
	for {
		select {
		case <-c.destroyCh:
			return
		default:
			node, meta, err := c.hub.nomad.Client.Nodes().Info(nodeID, q)
			if err != nil {
				logger.Errorf("connection: unable to fetch node info: %s", err)
				time.Sleep(10 * time.Second)
				continue
			}
			if !c.watches.Has(nodeID) {
				return
			}
			c.send <- &Action{Type: fetchedNode, Payload: node}

			waitIndex := meta.LastIndex
			if q.WaitIndex > meta.LastIndex {
				waitIndex = q.WaitIndex
			}
			q = &api.QueryOptions{WaitIndex: waitIndex, WaitTime: 10 * time.Second}
		}
	}
}

func (c *Connection) watchJob(action Action) {
	jobID := action.Payload.(string)

	defer func() {
		c.watches.Remove(jobID)
		logger.Infof("Stopped watching job with id: %s", jobID)
	}()
	c.watches.Add(jobID)

	logger.Infof("Started watching job with id: %s", jobID)

	q := &api.QueryOptions{WaitIndex: 1}
	for {
		select {
		case <-c.destroyCh:
			return
		default:
			job, meta, err := c.hub.nomad.Client.Jobs().Info(jobID, q)
			if err != nil {
				logger.Errorf("connection: unable to fetch job info: %s", err)
				time.Sleep(10 * time.Second)
				continue
			}
			if !c.watches.Has(jobID) {
				return
			}
			nomadAddr := os.Getenv("NOMAD_ADDR")
			nomadAddrArray := strings.Split(nomadAddr, ":")
			pureAddrArray := strings.Split(nomadAddrArray[1], "/")
			pureAddr := pureAddrArray[2]

			jenkinsURL := os.Getenv("JENKINS_BASE_URL")

			resultPayload := jobWithNomadAddrAndJenkinsURL{
				Region:            job.Region,
				ID:                job.ID,
				Name:              job.Name,
				Type:              job.Type,
				Priority:          job.Priority,
				AllAtOnce:         job.AllAtOnce,
				Datacenters:       job.Datacenters,
				Constraints:       job.Constraints,
				TaskGroups:        job.TaskGroups,
				Update:            job.Update,
				Periodic:          job.Periodic,
				Meta:              job.Meta,
				Status:            job.Status,
				StatusDescription: job.StatusDescription,
				CreateIndex:       job.CreateIndex,
				ModifyIndex:       job.ModifyIndex,
				NomadAddr:         pureAddr,
				JenkinsURL:        jenkinsURL,
			}

			c.send <- &Action{Type: fetchedJob, Payload: resultPayload}

			waitIndex := meta.LastIndex
			if q.WaitIndex > meta.LastIndex {
				waitIndex = q.WaitIndex
			}
			q = &api.QueryOptions{WaitIndex: waitIndex, WaitTime: 10 * time.Second}
		}
	}
}

func (c *Connection) fetchDir(action Action) {
	params, ok := action.Payload.(map[string]interface{})
	if !ok {
		logger.Errorf("Could not decode payload")
		return
	}
	addr := params["addr"].(string)
	path := params["path"].(string)
	allocID := params["allocID"].(string)

	config := api.DefaultConfig()
	config.Address = fmt.Sprintf("http://%s", addr)

	client, err := api.NewClient(config)
	if err != nil {
		logger.Fatalf("Could not create client: %s", err)
		return
	}
	alloc, _, err := client.Allocations().Info(allocID, nil)
	if err != nil {
		logger.Errorf("Unable to fetch alloc: %s", err)
		return
	}
	dir, _, err := client.AllocFS().List(alloc, path, nil)
	if err != nil {
		logger.Errorf("Unable to fetch directory: %s", err)
	}

	c.send <- &Action{Type: fetchedDir, Payload: dir}
}

type Line struct {
	Data string
	File string
}

func (c *Connection) watchFile(action Action) {
	params, ok := action.Payload.(map[string]interface{})
	if !ok {
		logger.Error("Could not decode payload")
		return
	}
	addr := params["addr"].(string)
	path := params["path"].(string)
	allocID := params["allocID"].(string)

	config := api.DefaultConfig()
	config.Address = fmt.Sprintf("http://%s", addr)

	client, err := api.NewClient(config)
	if err != nil {
		logger.Errorf("Could not create client: %s", err)
		return
	}
	alloc, _, err := client.Allocations().Info(allocID, nil)
	if err != nil {
		logger.Errorf("Unable to fetch alloc: %s", err)
		return
	}

	// Get file stat info
	file, _, err := client.AllocFS().Stat(alloc, path, nil)
	if err != nil {
		logger.Errorf("Unable to stat file: %s", err)
		return
	}

	var origin string = api.OriginStart
	var offset int64 = 0
	var oversized bool
	if file.Size > maxFileSize {
		origin = api.OriginEnd
		offset = maxFileSize
		oversized = true
	}

	cancel := make(chan struct{})
	frames, err := client.AllocFS().Stream(alloc, path, origin, offset, cancel, nil)
	if err != nil {
		c.send <- &Action{
			Type: fileStreamFailed,
			Payload: struct {
				path string
			}{
				path: path,
			},
		}

		logger.Errorf("Unable to stream file: %s", err)
		return
	}

	var r io.ReadCloser
	frameReader := api.NewFrameReader(frames, cancel)
	frameReader.SetUnblockTime(500 * time.Millisecond)
	r = command.NewLineLimitReader(frameReader, int(defaultTailLines), int(defaultTailLines*bytesToLines), 1*time.Second)

	// Turn the reader into a channel
	lines := make(chan []byte)
	b := make([]byte, defaultTailLines*bytesToLines)
	go func() {
		for {
			n, err := r.Read(b[:cap(b)])
			if err != nil {
				return
			}
			if n > 0 {
				lines <- b[0:n]
			}
		}
	}()

	c.watches.Add(path)
	defer func() {
		logger.Infof("Stopped watching file with path: %s", path)
		c.watches.Remove(path)
		r.Close()
	}()

	logger.Infof("Started watching file with path: %s", path)
	c.send <- &Action{
		Type: fetchedFile,
		Payload: struct {
			File      string
			Data      string
			Oversized bool
		}{
			File:      path,
			Data:      "",
			Oversized: oversized,
		},
	}

	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-c.destroyCh:
			return
		case line := <-lines:
			if !c.watches.Has(path) {
				return
			}
			c.send <- &Action{
				Type: fetchedFile,
				Payload: struct {
					File      string
					Data      string
					Oversized bool
				}{
					File:      path,
					Data:      string(line),
					Oversized: oversized,
				},
			}
		case <-ticker.C:
			if !c.watches.Has(path) {
				return
			}
		}
	}
}
