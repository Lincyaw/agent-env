//go:build scheduler_plugin

package main

import (
	"os"

	"github.com/Lincyaw/agent-env/pkg/scheduler"
	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register"
	_ "k8s.io/component-base/metrics/prometheus/clientgo"
	_ "k8s.io/component-base/metrics/prometheus/version"
	schedulerapp "k8s.io/kubernetes/cmd/kube-scheduler/app"
)

func main() {
	command := schedulerapp.NewSchedulerCommand(
		schedulerapp.WithPlugin(scheduler.ImageLocalityPluginName, scheduler.NewFrameworkImageLocalityPlugin),
	)
	os.Exit(cli.Run(command))
}
