package main

import (
	"log"
	"os"
	"runtime/pprof"
	rtrace "runtime/trace"

	"github.com/CiscoDevNet/terraform-provider-mso/mso"
	"github.com/hashicorp/terraform-plugin-sdk/plugin"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

func main() {

	// Create a trace file
	f, errtrace := os.Create("trace_tf_provider.out")
	if errtrace != nil {
		log.Fatalf("failed to create trace file: %v", errtrace)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalf("failed to close trace file: %v", err)
		}
	}()

	// Start tracing
	if errtrace := rtrace.Start(f); errtrace != nil {
		log.Fatalf("failed to start tracing: %v", errtrace)
	}
	defer rtrace.Stop()

	// CPU profiling to file
	cpuFile, err := os.Create("cpu_profile.out")
	if err != nil {
		log.Fatalf("failed to create CPU profile: %v", err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		log.Fatalf("failed to start CPU profile: %v", err)
	}
	defer pprof.StopCPUProfile()

	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: func() terraform.ResourceProvider {
			return mso.Provider()
		},
	})
}
