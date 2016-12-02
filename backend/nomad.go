package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/gorilla/mux"
	"github.com/hashicorp/nomad/api"
	uuid "github.com/satori/go.uuid"

	"math"
)

const (
	waitTime = 1 * time.Minute
)

// Wrapper around AgentMember that provides ID field. This is made to keep everything
// consistent i.e. other types have ID field.
type AgentMemberWithID struct {
	api.AgentMember
	ID     string
	Leader bool
}

type NomadUIJob struct {
	ID                string
	ParentID          string
	Name              string
	Type              string
	Priority          int
	Status            string
	StatusDescription string
	JobSummary        *api.JobSummary
	CreateIndex       uint64
	ModifyIndex       uint64
	JobModifyIndex    uint64
	DefaultPodName    string
}

func NewAgentMemberWithID(member *api.AgentMember) (*AgentMemberWithID, error) {
	h := md5.New() // we use md5 as it also has 16 bytes and it maps nicely to uuid

	_, err := io.WriteString(h, member.Name)
	if err != nil {
		return nil, err
	}

	_, err = io.WriteString(h, member.Addr)
	if err != nil {
		return nil, err
	}

	err = binary.Write(h, binary.LittleEndian, member.Port)
	if err != nil {
		return nil, err
	}

	sum := h.Sum(nil)
	ID, err := uuid.FromBytes(sum)
	if err != nil {
		return nil, err
	}

	return &AgentMemberWithID{
		AgentMember: *member,
		ID:          ID.String(),
		Leader:      false,
	}, nil
}

// Nomad keeps track of the Nomad state. It monitors changes to allocations,
// evaluations, jobs and nodes and broadcasts them to all connected websockets.
// It also exposes an API client for the Nomad server.
type Nomad struct {
	Client *api.Client

	allocs     []*api.AllocationListStub
	evals      []*api.Evaluation
	nodes      []*api.NodeListStub
	members    []*AgentMemberWithID
	usage      *NomadClusterUsage
	nomad_jobs []*NomadUIJob
	nomad_addr string
	updateCh   chan *Action
}

type NomadClusterUsage struct {
	CpuAvailable  float64
	CpuUsed       float64
	MemAvailable  float64
	MemUsed       float64
	DiskAvailable float64
	DiskUsed      float64
}

// NewNomad configures the Nomad API client and initializes the internal state.
func NewNomad(url string, updateCh chan *Action) (*Nomad, error) {
	config := api.DefaultConfig()
	config.Address = url
	config.WaitTime = waitTime

	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &Nomad{
		Client:     client,
		updateCh:   updateCh,
		allocs:     make([]*api.AllocationListStub, 0),
		evals:      make([]*api.Evaluation, 0),
		nodes:      make([]*api.NodeListStub, 0),
		members:    make([]*AgentMemberWithID, 0),
		nomad_jobs: make([]*NomadUIJob, 0),
	}, nil
}

// MembersWithID is used to query all of the known server members.
func (n *Nomad) MembersWithID() ([]*AgentMemberWithID, error) {
	members, err := n.Client.Agent().Members()
	if err != nil {
		return nil, err
	}

	ms := make([]*AgentMemberWithID, 0, len(members.Members))
	for _, m := range members.Members {
		x, err := NewAgentMemberWithID(m)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("Failed to create AgentMemberWithID %s: %#v", err, m))
		}
		ms = append(ms, x)
	}

	leader, err := n.Client.Status().Leader()
	if err != nil {
		logger.Error("Failed to fetch leader.")
		return nil, err
	}

	if leader != "" {
		parts := strings.Split(leader, ":")
		if len(parts) != 2 {
			return nil, errors.New(fmt.Sprintf("Failed to parse leader: %s", leader))
		}
		addr, port := parts[0], parts[1]

		for _, m := range ms {
			mPort, ok := m.Tags["port"]
			if ok && (mPort == port) && (m.Addr == addr) {
				m.Leader = true
			}
		}
	}
	return ms, nil
}

// MemberWithID is used to query a server member by its ID.
func (n *Nomad) MemberWithID(ID string) (*AgentMemberWithID, error) {
	members, err := n.MembersWithID()
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.ID == ID {
			return m, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("Unable to find member with ID: %s", ID))
}

// FlushAll sends the current Nomad state to the connection. This is used to pass
// all known state to the client connection.
func (n *Nomad) FlushAll(c *Connection) {
	c.send <- &Action{Type: fetchedAllocs, Payload: n.allocs}
	c.send <- &Action{Type: fetchedEvals, Payload: n.evals}
	c.send <- &Action{Type: fetchedJobs, Payload: n.nomad_jobs}
	c.send <- &Action{Type: fetchedNodes, Payload: n.nodes}
	c.send <- &Action{Type: fetchedMembers, Payload: n.members}
}

func (n *Nomad) watchAllocs() {
	q := &api.QueryOptions{WaitIndex: 1}

	for {
		allocs, meta, err := n.Client.Allocations().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch allocations: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		n.allocs = allocs
		n.updateCh <- &Action{Type: fetchedAllocs, Payload: allocs}

		// Guard for zero LastIndex in case of timeout
		waitIndex := meta.LastIndex
		if q.WaitIndex > meta.LastIndex {
			waitIndex = q.WaitIndex
		}
		q = &api.QueryOptions{WaitIndex: waitIndex}
	}
}

func (n *Nomad) watchEvals() {
	q := &api.QueryOptions{WaitIndex: 1}
	for {
		evals, meta, err := n.Client.Evaluations().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch evaluations: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}
		n.evals = evals
		n.updateCh <- &Action{Type: fetchedEvals, Payload: evals}

		// Guard for zero LastIndex in case of timeout
		waitIndex := meta.LastIndex
		if q.WaitIndex > meta.LastIndex {
			waitIndex = q.WaitIndex
		}
		q = &api.QueryOptions{WaitIndex: waitIndex}
	}
}

func (n *Nomad) watchJobs() {
	q := &api.QueryOptions{WaitIndex: 1}

	nomadAddr := os.Getenv("NOMAD_ADDR")
	nomadAddrArray := strings.Split(nomadAddr, ":")
	pureAddrArray := strings.Split(nomadAddrArray[1], "/")
	log.Printf("pureAddrArray: %v", pureAddrArray[2])

	consulAddr := os.Getenv("CONSUL_ADDR")
	log.Printf("consul Addr: %v", consulAddr)
	consulClient, err := NewConsulClient(consulAddr)
	if err != nil {
		log.Printf("get consul client error: %v", err)
	}

	for {
		jobs, meta, err := n.Client.Jobs().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch jobs: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// get keys from deafult pod kv pairs
		keys, _, err := consulClient.KV().Keys("cobalt/services/default-pod/", "", nil)
		if err != nil {
			logger.Errorf("watch: unable to fetch consul keys: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// mark job with default-pod key as DefaultPod
		m := make(map[string]bool)
		dpNameMap := make(map[string]string)

		for _, key := range keys {
			k, _, err := consulClient.KV().Get(key, nil)
			if err != nil {
				logger.Errorf("watch: unable to fetch key: %v", key)
			}
			log.Printf("k value:%v", string(k.Value))
			m[string(k.Value)] = true
			keyArray := strings.Split(key, "/")

			dpNameMap[string(k.Value)] = keyArray[len(keyArray)-1]

		}
		var nomad_jobs []*NomadUIJob
		for _, job := range jobs {

			nomad_job := &NomadUIJob{
				ID:                job.ID,
				ParentID:          job.ParentID,
				Name:              job.Name,
				Type:              job.Type,
				Priority:          job.Priority,
				Status:            job.Status,
				StatusDescription: job.StatusDescription,
				JobSummary:        job.JobSummary,
				CreateIndex:       job.CreateIndex,
				ModifyIndex:       job.ModifyIndex,
				JobModifyIndex:    job.JobModifyIndex,
			}

			shortJobID := nomad_job.ID[:strings.LastIndex(nomad_job.ID, "_")]

			if _, ok := m[shortJobID]; ok {
				nomad_job.DefaultPodName = dpNameMap[shortJobID]
			}

			nomad_jobs = append(nomad_jobs, nomad_job)
		}

		n.nomad_jobs = nomad_jobs
		n.updateCh <- &Action{Type: fetchedJobs, Payload: nomad_jobs}

		// Guard for zero LastIndex in case of timeout
		waitIndex := meta.LastIndex
		if q.WaitIndex > meta.LastIndex {
			waitIndex = q.WaitIndex
		}
		q = &api.QueryOptions{WaitIndex: waitIndex}
	}
}

func (n *Nomad) watchNodes() {
	q := &api.QueryOptions{WaitIndex: 1}
	for {
		nodes, meta, err := n.Client.Nodes().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch nodes: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}
		n.nodes = nodes
		n.updateCh <- &Action{Type: fetchedNodes, Payload: nodes}
		// Guard for zero LastIndex in case of timeout
		waitIndex := meta.LastIndex
		if q.WaitIndex > meta.LastIndex {
			waitIndex = q.WaitIndex
		}
		q = &api.QueryOptions{WaitIndex: waitIndex}
	}
}

func (n *Nomad) watchMembers() {
	for {
		members, err := n.MembersWithID()
		if err != nil {
			logger.Errorf("watch: unable to fetch members: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		n.members = members
		n.updateCh <- &Action{Type: fetchedMembers, Payload: members}

		time.Sleep(10 * time.Second)
	}
}

func (n *Nomad) downloadFile(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	path := params["path"]

	c := r.URL.Query().Get("client")
	allocID := r.URL.Query().Get("allocID")
	if c == "" || allocID == "" {
		http.Error(w, "client or allocID should be passed.", http.StatusBadRequest)
		return
	}

	config := api.DefaultConfig()
	config.Address = fmt.Sprintf("http://%s", c)

	client, err := api.NewClient(config)
	if err != nil {
		logger.Errorf("Could not create client: %s", err)
		http.Error(w, "Could not connect to Nomad client.", http.StatusInternalServerError)
		return
	}

	alloc, _, err := client.Allocations().Info(allocID, nil)
	if err != nil {
		logger.Errorf("Unable to fetch alloc: %s", err)
		http.Error(w, "Could not fetch the allocation.", http.StatusInternalServerError)
		return
	}

	file, err := client.AllocFS().Cat(alloc, path, nil)
	if err != nil {
		logger.Errorf("Unable to cat file: %s", err)
		http.Error(w, "Could not fetch the file.", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(path))
	w.Header().Set("Content-Type", "application/octet-stream")

	logger.Infof("download: streaming %q to client", path)

	io.Copy(w, file)
}

func (n *Nomad) watchUsages() {
	q := &api.QueryOptions{WaitIndex: 1}
	for {
		nodes, _, err := n.Client.Nodes().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch nodes: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var cpuUsed float64 = 0.0
		var cpuTotal float64 = 0.0
		var memTotal float64 = 0.0
		var memUsed float64 = 0.0
		var diskTotal float64 = 0.0
		var diskUsed float64 = 0.0

		for _, node := range nodes {
			host, _, e := n.Client.Nodes().Info(node.ID, q)
			if e != nil {
				logger.Errorf("watch: unable to fetch host: %s", e)
				time.Sleep(10 * time.Second)
				continue
			}
			cpuTotal += float64(host.Resources.CPU) / 1024.0
			memTotal += float64(host.Resources.MemoryMB) / 1024.0
			diskTotal += float64(host.Resources.DiskMB) / 1024.0

		}

		allocs, _, err := n.Client.Allocations().List(nil)
		if err != nil {
			logger.Errorf("watch: unable to fetch allocs: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, alloc := range allocs {
			allocation, _, e := n.Client.Allocations().Info(alloc.ID, nil)
			if e != nil {
				logger.Errorf("watch: unable to fetch allocation: %s", e)
				time.Sleep(10 * time.Second)
				continue
			}
			if allocation.ClientStatus == "running" {
				cpuUsed += float64(allocation.Resources.CPU) / 1024.0
				memUsed += float64(allocation.Resources.MemoryMB) / 1024.0
				diskUsed += float64(allocation.Resources.DiskMB) / 1024.0
			}
		}

		usage := &NomadClusterUsage{
			CpuAvailable:  toFixed(cpuTotal-cpuUsed, 2),
			CpuUsed:       toFixed(cpuUsed, 2),
			MemAvailable:  toFixed(memTotal-memUsed, 2),
			MemUsed:       toFixed(memUsed, 2),
			DiskAvailable: toFixed(diskTotal-diskUsed, 2),
			DiskUsed:      toFixed(diskUsed, 2),
		}

		n.usage = usage
		n.updateCh <- &Action{Type: fetchedUsage, Payload: usage}

		time.Sleep(10 * time.Second)
	}
}

func (n *Nomad) watchUsagesWithNewNomadAPI() {
	q := &api.QueryOptions{WaitIndex: 1}
	for {
		nodes, _, err := n.Client.Nodes().List(q)
		if err != nil {
			logger.Errorf("watch: unable to fetch nodes: %s", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var cpuUsed float64 = 0.0
		var cpuTotal float64 = 0.0
		var memAvailable float64 = 0
		var memUsed float64 = 0
		var diskAvailable float64 = 0.0
		var diskUsed float64 = 0.0

		for _, node := range nodes {
			host, _, e := n.Client.Nodes().Info(node.ID, q)
			if e != nil {
				logger.Errorf("watch: unable to fetch host: %s", e)
				time.Sleep(10 * time.Second)
				continue
			}
			cpuTotal += float64(host.Resources.CPU) / 1024.0
			hostStat, e := n.Client.Nodes().Stats(node.ID, q)
			if e != nil {
				logger.Errorf("watch: unable to fetch host stats: %s", e)
				time.Sleep(10 * time.Second)
				continue
			}

			// Byte -> MB -> GB
			memAvailable += float64(hostStat.Memory.Available) / 1024.0 / 1024.0 / 1024.0
			memUsed += float64(hostStat.Memory.Used) / 1024.0 / 1024.0 / 1024.0

			// Byte -> MB -> GB
			for _, disk := range hostStat.DiskStats {
				diskAvailable += float64(disk.Available) / 1024.0 / 1024.0 / 1024.0

				diskUsed += float64(disk.Used) / 1024.0 / 1024.0 / 1024.0
			}

			// GHz
			cpuUsed += hostStat.CPUTicksConsumed / 1024.0
		}
		usage := &NomadClusterUsage{
			CpuAvailable:  toFixed(cpuTotal-cpuUsed, 2),
			CpuUsed:       toFixed(cpuUsed, 2),
			MemAvailable:  toFixed(memAvailable, 2),
			MemUsed:       toFixed(memUsed, 2),
			DiskAvailable: toFixed(diskAvailable, 2),
			DiskUsed:      toFixed(diskUsed, 2),
		}
		n.usage = usage
		n.updateCh <- &Action{Type: fetchedUsage, Payload: usage}

		time.Sleep(10 * time.Second)
	}
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}
