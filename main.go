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
	"github.com/rs/zerolog"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var log = zerolog.New(os.Stdout).
	With().Timestamp().Str("service", "local-service").
		Logger().Level(zerolog.InfoLevel)

var (
	fs          = flag.NewFlagSet("local_service", flag.ExitOnError)
	jobspecFile = fs.String("jobspec", "nomad-jobspec.tmpl", "Specify jobspec to use if not default")
	iface = fs.String("iface", "", "Specify address by interface")
	debug = fs.Bool("debug",false,"Add extra logging")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {

	err := fs.Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing flags: %v", err)
	}
	if *debug {
		log.Level(zerolog.DebugLevel)
	}

	consul, err := consultant.NewDefaultClient()
	if err != nil {
		log.Fatal().Msgf("Couldn't build a consul client, that's a real problem: %v", err)
	}
	job, err := jobspec.ParseFile(*jobspecFile)
	if err != nil {
		log.Fatal().Msgf("Error when trying to parse %s: %v", jobspecFile, err)
	}

	taskGroups := job.TaskGroups

	myAddress,err := util.MyAddress()
	if err != nil {
		log.Fatal().Msgf("Unable to determine the address of this host: %s", err)
	}

	for k := range taskGroups {
		log.Info().Msgf("Processing task group: %s", *taskGroups[k].Name)
		taskList := taskGroups[k].Tasks
		for l := range taskList {
			log.Info().Msgf("Diving into task list: %s", taskList[l].Name)
			serviceList := taskList[l].Services
			for _, s := range serviceList {
				log.Info().Msgf("Looking at service: %s", s.Name)

				port, err := strconv.Atoi(s.PortLabel)
				if err != nil {
					log.Warn().Msgf("Service port label not a number (%s): %s", s.PortLabel, err)
					continue
				}

				checks := buildChecks(s, myAddress)

				asr := &capi.AgentServiceRegistration{
					ID: serviceId(s.Name),
					Name: s.Name,
					Tags: s.Tags,
					Port: port,
					Address: myAddress,
					Checks: checks,
				}

				j,_ := json.Marshal(asr)
				// this is clearly not the correct way to use zerolog
				log.Info().RawJSON("payload",j).Msg("write")

				err = consul.Agent().ServiceRegister(asr)
				defer func(id string) {
					err := consul.Agent().ServiceDeregister(id)
					if err != nil {
						log.Info().Msgf("Error deregistering %s",id)
					} else {
						log.Info().Msgf("Deregistered %s",id)
					}
				}(asr.ID)
				if err != nil {
					log.Warn().Msgf("Something went wrong when trying to register the service: %v",err)
				}
			}
		}
	}

	// Wait for a signal to exit
	log.Info().Msg("Now just waiting for the end")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sig := <-ch
	log.Info().Msgf("Got a signal (%s), exiting\n", sig)

}

func serviceId(serviceName string) string {
	return fmt.Sprintf("%s-%16x", serviceName, rand.Int63())
}

func buildChecks(s *napi.Service, myAddress string) []*capi.AgentServiceCheck {
	checks := capi.AgentServiceChecks{}
	for _, c := range s.Checks {
		asc := &capi.AgentServiceCheck{
			CheckID: c.Id,
			Name: c.Name,
			Interval: c.Interval.String(),
		}

		checkPort, err := strconv.Atoi(c.PortLabel)
		if err != nil {
			log.Warn().Msgf("Check port is not a number (%s): %v", c.PortLabel, err)
			continue
		}

		switch c.Type {
		case "http":
			u := url.URL{
				Scheme: "http",
				Host: fmt.Sprintf("%s:%d", myAddress, checkPort),
				Path: c.Path,
			}
			asc.HTTP = u.String()
		default:
			log.Warn().Msgf("Unhandled check method: %s", c.Method)
		}

		checks = append(checks, asc)
	}

	return checks

}