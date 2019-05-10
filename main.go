package main

import (
	"encoding/json"
	"flag"
	"fmt"
	capi "github.com/hashicorp/consul/api"
	napi "github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/jobspec"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/util"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var (
	fs          = flag.NewFlagSet("local_service", flag.ExitOnError)
	jobspecFile = fs.String("jobspec", "nomad-jobspec.tmpl", "Specify jobspec to use if not default")
	iface       = fs.String("iface", "", "Specify address by interface")
	debug       = fs.Bool("debug", false, "Add extra logging")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {

	err := fs.Parse(os.Args[1:])
	if err != nil {
		fmt.Printf("Error parsing flags: %v\n", err)
	}

	consul, err := consultant.NewDefaultClient()
	if err != nil {
		fmt.Printf("Couldn't build a consul client, that's a real problem: %v\n", err)
		os.Exit(1)
	}
	job, err := jobspec.ParseFile(*jobspecFile)
	if err != nil {
		fmt.Printf("Error when trying to parse %s: %v\n", jobspecFile, err)
		os.Exit(1)
	}

	taskGroups := job.TaskGroups

	myAddress, err := util.MyAddress()
	if err != nil {
		fmt.Printf("Unable to determine the address of this host: %s\n", err)
		os.Exit(1)
	}

	for k := range taskGroups {
		fmt.Printf("Processing task group: %s\n", *taskGroups[k].Name)
		taskList := taskGroups[k].Tasks
		for l := range taskList {
			fmt.Printf("> Diving into task list: %s\n", taskList[l].Name)
			serviceList := taskList[l].Services
			for _, s := range serviceList {
				fmt.Printf("> > Looking at service: %s\n", s.Name)

				port, err := strconv.Atoi(s.PortLabel)
				if err != nil {
					fmt.Printf("> > Service port label not a number (%s): %s\n", s.PortLabel, err)
					continue
				}

				checks := buildChecks(s, myAddress)

				asr := &capi.AgentServiceRegistration{
					ID:      serviceId(s.Name),
					Name:    s.Name,
					Tags:    s.Tags,
					Port:    port,
					Address: myAddress,
					Checks:  checks,
				}

				j, _ := json.MarshalIndent(asr,"> > > ","  ")
				fmt.Printf("> > Registration: %s\n",string(j))

				err = consul.Agent().ServiceRegister(asr)
				defer func(id string) {
					err := consul.Agent().ServiceDeregister(id)
					if err != nil {
						fmt.Printf("Error deregistering %s\n", id)
					} else {
						fmt.Printf("Deregistered %s\n", id)
					}
				}(asr.ID)
				if err != nil {
					fmt.Printf("> ? Something went wrong when trying to register the service: %v\n", err)
				}
			}
		}
	}

	// Wait for a signal to exit
	fmt.Println("Now just waiting for the end")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sig := <-ch
	fmt.Printf("Got a signal (%s), exiting\n", sig)

}

func serviceId(serviceName string) string {
	return fmt.Sprintf("%s-%16x", serviceName, rand.Int63())
}

func buildChecks(s *napi.Service, myAddress string) []*capi.AgentServiceCheck {
	checks := capi.AgentServiceChecks{}
	for _, c := range s.Checks {
		asc := &capi.AgentServiceCheck{
			CheckID:  c.Id,
			Name:     c.Name,
			Interval: c.Interval.String(),
		}

		checkPort, err := strconv.Atoi(c.PortLabel)
		if err != nil {
			fmt.Printf("> > > Check port is not a number (%s): %v\n", c.PortLabel, err)
			continue
		}

		switch c.Type {
		case "http":
			u := url.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%d", myAddress, checkPort),
				Path:   c.Path,
			}
			asc.HTTP = u.String()
		default:
			fmt.Printf("> > > Unhandled check method: %s\n", c.Method)
		}

		checks = append(checks, asc)
	}

	return checks

}
