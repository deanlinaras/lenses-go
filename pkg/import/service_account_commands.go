package imports

import (
	"fmt"

	"github.com/kataras/golog"
	"github.com/landoop/bite"
	"github.com/landoop/lenses-go/pkg"
	"github.com/landoop/lenses-go/pkg/api"
	config "github.com/landoop/lenses-go/pkg/configs"
	"github.com/landoop/lenses-go/pkg/utils"
	"github.com/spf13/cobra"
)

type importServiceAccount struct {
	Name  string `json:"name" yaml:"name" header:"Name"`
	Token string `json:"token" yaml:"token" header:"token"`
}

//NewImportServiceAccountsCommand creates `import serviceaccounts` command
func NewImportServiceAccountsCommand() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:              "serviceaccounts",
		Short:            "serviceaccounts",
		Example:          `import serviceaccounts --dir users`,
		SilenceErrors:    true,
		TraverseChildren: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			path = fmt.Sprintf("%s/%s", path, pkg.ServiceAccountsPath)
			if err := loadServiceAccounts(config.Client, cmd, path); err != nil {
				golog.Errorf("Failed to load user groups. [%s]", err.Error())
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "dir", ".", "Base directory to import")

	bite.CanPrintJSON(cmd)
	return cmd
}

func loadServiceAccounts(client *api.Client, cmd *cobra.Command, loadpath string) error {
	golog.Infof("Loading service accounts from [%s]", loadpath)
	files := utils.FindFiles(loadpath)

	currentSvcAccs, err := client.GetServiceAccounts()

	if err != nil {
		return err
	}

	for _, file := range files {

		var svcacc api.ServiceAccount
		if err := bite.LoadFile(cmd, fmt.Sprintf("%s/%s", loadpath, file.Name()), &svcacc); err != nil {
			golog.Errorf("Error loading file [%s]", loadpath)
			return err
		}

		found := false
		for _, sva := range currentSvcAccs {
			if sva.Name == svcacc.Name {
				found = true

				payload := &api.ServiceAccount{
					Name:   svcacc.Name,
					Owner:  svcacc.Owner,
					Groups: svcacc.Groups,
				}

				if err := config.Client.UpdateServiceAccount(payload); err != nil {
					golog.Errorf("Error updating service account [%s]. [%s]", svcacc.Name, err.Error())
					return err
				}
				golog.Infof("Updated service account [%s]", svcacc.Name)
			}
		}

		if found {
			continue
		}

		payload, err := client.CreateServiceAccount(&svcacc)
		if err != nil {
			golog.Errorf("Error creating service account [%s] from [%s] [%s]", svcacc.Name, loadpath, err.Error())
			return err
		}
		golog.Infof("Created service account [%s], Token:[%s]", svcacc.Name, payload.Token)
	}

	return nil
}
