// Copyright 2018 The Containerfs Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"strings"

	"github.com/tiglabs/containerfs/datanode"
	"github.com/tiglabs/containerfs/master"
	"github.com/tiglabs/containerfs/metanode"
	"github.com/tiglabs/containerfs/util/log"

	"flag"
	"fmt"
	"github.com/tiglabs/containerfs/util/config"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

var (
	Version = "0.01"
)

const (
	ConfigKeyRole         = "role"
	ConfigKeyLogDir       = "logDir"
	ConfigKeyLogLevel     = "logLevel"
	ConfigKeyProfPort     = "prof"
	ConfigKeyExporterPort = "exporterPort"
)

const (
	RoleMaster = "master"
	RoleMeta   = "metanode"
	RoleData   = "datanode"
)

const (
	ModuleMaster = "master"
	ModuleMeta   = "metaNode"
	ModuleData   = "dataNode"
)

var (
	configFile    = flag.String("c", "", "config file path")
	configVersion = flag.Bool("v", false, "show version")
)

type Server interface {
	Start(cfg *config.Config) error
	Shutdown()
	// Sync will block invoker goroutine until this MetaNode shutdown.
	Sync()
}

func interceptSignal(s Server) {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	log.LogInfo("action[interceptSignal] register system signal.")
	go func() {
		sig := <-sigC
		log.LogInfo("action[interceptSignal] received signal: %s.", sig.String())
		s.Shutdown()
	}()
}

func modifyOpenFiles() (err error) {
	var rLimit syscall.Rlimit
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Getting Rlimit %v", err.Error())
	}
	fmt.Println(rLimit)
	rLimit.Max = 1024000
	rLimit.Cur = 1024000
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Setting Rlimit %v", err.Error())
	}
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Getting Rlimit %v", err.Error())
	}
	fmt.Println("Rlimit Final", rLimit)
	return
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.LogErrorf("action[main] process panic detail: %v.", r)
			log.LogFlush()
			panic(r)
		}
	}()
	flag.Parse()
	err := modifyOpenFiles()
	if err != nil {
		panic(err.Error())
	}
	if *configVersion {
		fmt.Printf("Current Verson: %s\n", Version)
		os.Exit(0)
		return
	}
	log.LogInfof("Hello, Cfs Storage, Current Version: %s", Version)
	cfg := config.LoadConfigFile(*configFile)
	role := cfg.GetString(ConfigKeyRole)
	logDir := cfg.GetString(ConfigKeyLogDir)
	logLevel := cfg.GetString(ConfigKeyLogLevel)
	profPort := cfg.GetString(ConfigKeyProfPort)

	//for multi-cpu scheduling
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Init server instance with specified role configuration.
	var (
		server Server
		module string
	)
	switch role {
	case RoleMeta:
		server = metanode.NewServer()
		module = ModuleMeta
	case RoleMaster:
		server = master.NewServer()
		module = ModuleMaster
	case RoleData:
		server = datanode.NewServer()
		module = ModuleData
	default:
		fmt.Println("Fatal: role mismatch: ", role)
		os.Exit(1)
		return
	}

	// Init logging
	var (
		level log.Level
	)
	switch strings.ToLower(logLevel) {
	case "debug":
		level = log.DebugLevel
	case "info":
		level = log.InfoLevel
	case "warn":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	default:
		level = log.ErrorLevel
	}

	if profPort != "" {
		go func() {
			http.ListenAndServe(fmt.Sprintf(":%v", profPort), nil)
		}()
	}

	if _, err := log.InitLog(logDir, module, level, nil); err != nil {
		fmt.Println("Fatal: failed to init log - ", err)
		os.Exit(1)
		return
	}

	interceptSignal(server)
	err = server.Start(cfg)
	if err != nil {
		fmt.Println("Fatal: failed to start the baud storage daemon - ", err)
		log.LogFatal("Fatal: failed to start the baud storage daemon - ", err)
		log.LogFlush()
		os.Exit(1)
		return
	}
	// Block main goroutine until server shutdown.
	server.Sync()
	log.LogFlush()
	os.Exit(0)
}
