package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/chanwit/script"
	"github.com/pkg/errors"
)

func createClusterEKZ() error {
	// "5" is the latest stable provided by EKZ
	ekzImageBuild := "5"
	imageName := fmt.Sprintf("quay.io/ekz-io/ekz:%s.%s", eksdVersion, ekzImageBuild)
	containerName := fmt.Sprintf("%s-controller-0", clusterName)

	logger.Actionf("pulling image: %s ...", imageName)
	var err error
	err = script.Exec("docker", "pull", imageName).Run()
	if err != nil {
		return errors.Wrapf(err, "error pulling image: %s", imageName)
	}

	containerId := script.Var()
	err = script.Exec("docker", "ps", "-aq", "-f", "name="+containerName).To(containerId)
	if err != nil {
		return errors.Wrapf(err, "failed to run docker ps to check container: %s.", containerName)
	}

	// container existed
	if containerId.String() != "" {
		if verbose {
			fmt.Printf("[DEBUG] containerId.String()=%s\n", containerId.String())
		}

		return errors.Errorf("container %s existed - cluster creation aborted", containerName)
	}

	bridgeName := fmt.Sprintf("ekz-%s-bridge", clusterName)

	// TODO check if the bridge already existed

	logger.Actionf("creating bridge network: %s", bridgeName)
	err = script.Exec("docker", "network", "create",
		"-d", "bridge",
		"-o", "com.docker.network.bridge.enable_ip_masquerade=true",
		"-o", "com.docker.network.bridge.enable_icc=true",
		"-o", "com.docker.network.bridge.host_binding_ipv4=0.0.0.0",
		"-o", "com.docker.network.driver.mtu=1500",
		bridgeName).Run()
	if err != nil {
		return errors.Wrapf(err, "failed to create bridge: %s", bridgeName)
	}

	logger.Actionf("starting container: %s ...", containerName)
	_, stderr, err := script.Exec("docker", "run",
		"--detach",
		"--name", containerName,
		"--hostname", "controller",
		"--privileged",
		"--security-opt", "seccomp=unconfined", // also ignore seccomp
		"--security-opt", "apparmor=unconfined", // also ignore apparmor
		// runtime temporary storage
		"--tmpfs", "/tmp", // various things depend on working /tmp
		"--tmpfs", "/run", // systemd wants a writable /run
		// BUG failed: nameserver list is empty
		"--network", bridgeName,
		"--label", fmt.Sprintf("io.x-k8s.ekz.cluster=%s", clusterName),
		"--volume", "/var/lib/ekz",
		// some k8s things want to read /lib/modules
		"--volume", "/lib/modules:/lib/modules:ro",
		"-p", "127.0.0.1:0:6443",
		imageName).
		DividedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to start %s container with image: %s. %s", containerName, imageName, strings.TrimSpace(string(stderr)))
	}

	// TODO use retry-backoff instead of fixing 2 seconds here
	time.Sleep(2 * time.Second)

	// TODO handle port clash
	// TODO handle container name clash
	err = getKubeconfigEKZ(containerName, kubeConfigFile)
	if err != nil {
		return err
	}
	logger.Successf("kubeconfig is written to: %s", kubeConfigFile)

	logger.Waitingf("waiting for cluster to start ...")
	waitForNodeStarted("controller", 30*time.Second)

	logger.Waitingf("waiting for cluster to be ready ...")
	waitForNodeReady(60 * time.Second)

	logger.Successf("the EKS-D cluster is now ready.")
	return nil
}
