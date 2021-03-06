package docker

import (
	"errors"
	"fmt"
	"time"

	"github.com/wulog/engine/message"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/pborman/uuid"
	"github.com/wulog/engine/pipeline"
)

type DockerEventInputConfig struct {
	Endpoint string `toml:"endpoint"`
	CertPath string `toml:"cert_path"`
}

type DockerEventInput struct {
	conf         *DockerEventInputConfig
	dockerClient DockerClient
	eventStream  chan *docker.APIEvents
	stopChan     chan error
}

func (dei *DockerEventInput) ConfigStruct() interface{} {
	return &DockerEventInputConfig{
		Endpoint: "unix:///var/run/docker.sock",
		CertPath: "",
	}
}

func (dei *DockerEventInput) Init(config interface{}) error {
	dei.conf = config.(*DockerEventInputConfig)
	c, err := newDockerClient(dei.conf.CertPath, dei.conf.Endpoint)
	if err != nil {
		return fmt.Errorf("DockerEventInput: failed to attach to docker event API: %s", err.Error())
	}

	dei.dockerClient = c
	dei.eventStream = make(chan *docker.APIEvents)
	dei.stopChan = make(chan error)

	err = dei.dockerClient.AddEventListener(dei.eventStream)
	if err != nil {
		return fmt.Errorf("DockerEventInput: failed to add event listener: %s", err.Error())
	}
	return nil
}

func (dei *DockerEventInput) Run(ir pipeline.InputRunner, h pipeline.PluginHelper) error {
	defer dei.dockerClient.RemoveEventListener(dei.eventStream)
	defer close(dei.eventStream)
	var (
		ok   bool
		err  error
		pack *pipeline.PipelinePack
	)
	hostname := h.Hostname()

	// Provides empty PipelinePacks
	packSupply := ir.InChan()

	var event *docker.APIEvents
	ok = true
	for ok {
		select {
		case event, ok = <-dei.eventStream:
			if !ok {
				err = errors.New("DockerEventInput: eventStream channel closed")
				break
			}
			pack = <-packSupply
			pack.Message.SetType("DockerEvent")
			pack.Message.SetLogger(event.ID)
			pack.Message.SetHostname(hostname)

			payload := fmt.Sprintf("id:%s status:%s from:%s time:%d", event.ID, event.Status, event.From, event.Time)
			pack.Message.SetPayload(payload)
			pack.Message.SetTimestamp(time.Now().UnixNano())
			pack.Message.SetUuid(uuid.NewRandom())
			message.NewStringField(pack.Message, "ID", event.ID)
			message.NewStringField(pack.Message, "Status", event.Status)
			message.NewStringField(pack.Message, "From", event.From)
			message.NewInt64Field(pack.Message, "Time", event.Time, "ts")
			ir.Deliver(pack)
		case err = <-dei.stopChan:
			ok = false
		}
	}
	return err
}

func (dei *DockerEventInput) Stop() {
	close(dei.stopChan)
}

func (dei *DockerEventInput) CleanupForRestart() {
	// Intentially left empty. Cleanup happens in Run()
}

func init() {
	pipeline.RegisterPlugin("DockerEventInput", func() interface{} {
		return new(DockerEventInput)
	})
}
