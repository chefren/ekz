package main

import (
	"fmt"

	"github.com/chanwit/ekz/pkg/constants"
	"github.com/chanwit/ekz/pkg/kubeconfig"
	"github.com/chanwit/script"
	"github.com/imdario/mergo"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/kind/pkg/cluster"
)

var getKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Args:  cobra.MaximumNArgs(1),
	Short: "Get kubeconfig",
	Long:  "This command obtains the KubeConfig of the EKS-D cluster and writes to the target file.",
	Example: `  # Get the KubeConfig from the cluster and write to $HOME/.kube/config
  ekz get kubeconfig

  # Get the KubeConfig of the 'dev' cluster
  ekz get kubeconfig --name=dev

  # Get the KubeConfig of the 'dev' cluster (alternative syntax) 
  ekz get kubeconfig dev

  # Get the KubeConfig and save to $PWD/kubeconfig
  ekz get kubeconfig -o $PWD/kubeconfig

  # Get the KubeConfig from the default KIND-based cluster
  ekz get kubeconfig --provider=kind

  # Get the KubeConfig from the 'dev' KIND-based cluster
  ekz get kubeconfig --provider=kind --name=dev
`,
	RunE: getKubeconfigCmdRun,
}

func init() {
	getKubeconfigCmd.Flags().StringVarP(&kubeConfigFile, "output", "o", constants.BackTickHomeFile, "specify output file to write kubeconfig to")
	getKubeconfigCmd.Flags().StringVar(&clusterName, "name", "ekz", "cluster name")

	getCmd.AddCommand(getKubeconfigCmd)
}

func getKubeconfigCmdRun(cmd *cobra.Command, args []string) error {
	logger.Successf("the default provider is: %s", provider)

	// use args[0] as the clusterName
	if len(args) == 1 {
		clusterName = args[0]
	}

	if kubeConfigFile == constants.BackTickHomeFile {
		kubeConfigFile = clientcmd.RecommendedHomeFile
	}

	switch provider {
	case "ekz":
		containerName := fmt.Sprintf("%s-controller-0", clusterName)
		return getKubeconfigEKZ(containerName, kubeConfigFile)
	case "kind":
		return getKubeconfigKIND()
	}

	return nil
}

func getKubeconfigEKZ(containerName string, targetFile string) error {
	// TODO append kubeconfig to ~/.kube/config
	kubeconfigContent := script.Var()
	err := script.Exec("docker", "exec",
		containerName,
		"cat", "/var/lib/ekz/pki/admin.conf").
		To(kubeconfigContent)
	if err != nil {
		return errors.Wrapf(err, "error obtaining kubeconfig from container: %s", containerName)
	}

	networkName, err := getEKZNetworkName(containerName)
	if err != nil {
		return err
	}

	// host mode port locked to 6443 for EKZ/k0s
	port := "6443"
	if networkName != "host" {
		// Rewrite port of the API server inside the KubeConfig
		portVar := script.Var()
		err = script.Exec(
			"docker", "inspect",
			containerName,
			"--format", `{{ (index (index .NetworkSettings.Ports "6443/tcp") 0).HostPort }}`).
			To(portVar)
		if err != nil {
			return errors.Wrapf(err, "cannot obtain port mapping from docker")
		}

		port = portVar.String()
	}

	rewroteKubeconfig, err := kubeconfig.RewriteKubeConfigForEKZ(clusterName, kubeconfigContent.RawString(), port)
	if err != nil {
		return errors.Wrapf(err, "cannot obtain port mapping from docker")
	}

	// if cannot load from file
	// create an empty config
	config, err := clientcmd.LoadFromFile(targetFile)
	if err != nil {
		config = api.NewConfig()
	}

	newConfig, err := clientcmd.Load([]byte(rewroteKubeconfig))
	if err != nil {
		return err
	}
	err = mergo.Merge(config, newConfig, mergo.WithOverride)
	if err != nil {
		return err
	}
	err = clientcmd.WriteToFile(*config, targetFile)
	if err != nil {
		return err
	}

	return nil
}

func getKubeconfigKIND() error {
	provider := cluster.NewProvider()
	return provider.ExportKubeConfig(clusterName, kubeConfigFile)
}
