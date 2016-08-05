package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"k8s.io/kops/upup/pkg/api"
	"k8s.io/kops/upup/pkg/fi/vfs"
	"strings"
)

// CreateCmd represents the create command
type CreateCmd struct {
	filename string

	cobraCommand *cobra.Command
}

var createCmd = CreateCmd{
	cobraCommand: &cobra.Command{
		Use:        "create",
		SuggestFor: []string{"list"},
		Short:      "create objects",
		Long:       `create objects`,
	},
}

func init() {
	cmd := createCmd.cobraCommand

	rootCommand.AddCommand(cmd)

	cmd.Flags().StringVarP(&createCmd.filename, "filename", "f", "", "Filename to use to create the resource")

	cmd.Run = func(cmd *cobra.Command, args []string) {
		err := createCmd.Run(args)
		if err != nil {
			exitWithError(err)
		}
	}
}

func (c *CreateCmd) Run(args []string) error {
	if c.filename == "" {
		return fmt.Errorf("Syntax: kops create -f <filename>")
	}

	data, err := vfs.Context.ReadFile(c.filename)
	if err != nil {
		return fmt.Errorf("error reading file %q: %v", c.filename, err)
	}

	instanceGroupStore, err := rootCommand.InstanceGroupRegistry()
	if err != nil {
		return err
	}

	// TODO: Parse unversioned.TypeMeta ?
	generic := make(map[string]interface{})
	err = api.ParseYaml(data, &generic)
	if err != nil {
		return fmt.Errorf("error parsing yaml: %v", err)
	}
	kind := ""
	for k, v := range generic {
		if strings.ToLower(k) == "kind" {
			if vString, ok := v.(string); ok {
				kind = vString
			} else {
				return fmt.Errorf("Kind must be a string")
			}

		}
	}
	if kind == "" {
		return fmt.Errorf("cannot determine object Kind")
	}

	if strings.ToLower(kind) == strings.ToLower(api.KindInstanceGroup) {
		// TODO: DRY with create ig
		group := &api.InstanceGroup{}
		err = api.ParseYaml(data, group)
		if err != nil {
			return fmt.Errorf("error parsing yaml: %v", err)
		}

		err = group.Validate(false)
		if err != nil {
			return err
		}

		existing, err := instanceGroupStore.Find(group.Name)
		if err != nil {
			return err
		}

		if existing != nil {
			return fmt.Errorf("instance group %q already exists", group.Name)
		}

		err = instanceGroupStore.Create(group)
		if err != nil {
			return fmt.Errorf("error storing instancegroup: %v", err)
		}

		return nil
	}

	return fmt.Errorf("Unknown Kind: %v", kind)
}
