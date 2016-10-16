package main

import (
	"fmt"
	"github.com/spf13/cobra"
	k8sapi "k8s.io/kubernetes/pkg/api"
)

type DeleteFederationCmd struct {
	Yes        bool
	Unregister bool
}

var deleteFederation DeleteFederationCmd

func init() {
	cmd := &cobra.Command{
		Use:   "federation FEDERATIONNAME [--yes]",
		Short: "Delete federation",
		Long:  `Deletes a k8s federation.`,
		Run: func(cmd *cobra.Command, args []string) {
			err := deleteFederation.Run(args)
			if err != nil {
				exitWithError(err)
			}
		},
	}

	deleteCmd.AddCommand(cmd)

	cmd.Flags().BoolVar(&deleteFederation.Yes, "yes", false, "Delete without confirmation")
	cmd.Flags().BoolVar(&deleteFederation.Unregister, "unregister", false, "Don't delete cloud resources, just unregister the federation")
}

func (c *DeleteFederationCmd) Run(args []string) error {
	if len(args) == 0 {
		exitWithError(fmt.Errorf("Specify name of federation to delete"))
	}
	if len(args) != 1 {
		exitWithError(fmt.Errorf("Can only delete one federation at a time!"))
	}
	federationName := args[0]

	wouldDeleteCloudResources := false

	clientset, err := rootCommand.Clientset()
	if err != nil {
		return err
	}

	federation, err := clientset.Federations().Get(federationName)
	if err != nil {
		return err
	}

	if federation == nil {
		return fmt.Errorf("federation %q not found", federationName)
	}

	if federationName != federation.Name {
		return fmt.Errorf("sanity check failed: federation name mismatch")
	}

	if !c.Unregister {
		return fmt.Errorf("complete cluster deletion is not yet implemented; can only unregister")
	}

	if !c.Yes {
		if wouldDeleteCloudResources {
			fmt.Printf("\nMust specify --yes to delete resources & unregister federation\n")
		} else {
			fmt.Printf("\nMust specify --yes to unregister the federation\n")
		}
		return nil
	}

	options := &k8sapi.DeleteOptions{}

	err = clientset.Federations().Delete(federation.Name, options)
	if err != nil {
		return fmt.Errorf("error deleting federation object: %v", err)
	}

	fmt.Printf("\nFederation deleted\n")
	return nil
}
