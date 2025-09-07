package server

import (
	"fmt"
	"log"

	capi "github.com/hashicorp/consul/api"
)

type RegisterServer struct {
	ServiceID   string
	ServiceName string
	Addr        string
	Port        int
	client      *capi.Client
}

func newRegisterServer(serviceId, serviceName, addr string, port uint) *RegisterServer {
	config := capi.DefaultConfig()
	client, err := capi.NewClient(config)
	if err != nil {
		log.Fatalf("failed to create consul client: %v", err)
	}
	return &RegisterServer{
		ServiceID:   serviceId,
		ServiceName: serviceName,
		Addr:        addr,
		Port:        int(port),
		client:      client,
	}
}

func (r *RegisterServer) Run(in chan<- bool) {

	reg := &capi.AgentServiceRegistration{
		ID:      r.ServiceID,
		Name:    r.ServiceName,
		Address: r.Addr,
		Port:    r.Port,
		Check: &capi.AgentServiceCheck{
			GRPC:                           fmt.Sprintf("%s:%d/%s", r.Addr, r.Port, r.ServiceName),
			Interval:                       "10s",
			Timeout:                        "1s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}

	if err := r.client.Agent().ServiceRegister(reg); err != nil {
		in <- false
		log.Fatalf("failed to register service: %v", err)
	}
	log.Printf("Registered %s in Consul", r.ServiceID)

	in <- true

}

func (r *RegisterServer) End(out <-chan bool) bool {

	if err := r.client.Agent().ServiceDeregister(r.ServiceID); err != nil {
		log.Printf("failed to deregister: %v", err)
	} else {
		log.Printf("Deregistered %s from Consul", r.ServiceID)
	}

	return <-out

}
