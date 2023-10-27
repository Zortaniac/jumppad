package cmd

import (
	"os"

	"github.com/jumppad-labs/jumppad/pkg/clients"
	"github.com/jumppad-labs/jumppad/pkg/clients/connector"
	"github.com/jumppad-labs/jumppad/pkg/utils"
	"github.com/spf13/cobra"
)

func newDestroyCmd(cc connector.Connector) *cobra.Command {
	return &cobra.Command{
		Use:     "down",
		Short:   "Remove all resources in the current state",
		Long:    "Remove all resources in the current state",
		Example: `jumppad down`,
		Run: func(cmd *cobra.Command, args []string) {
			l := createLogger()
			engineClients, _ := clients.GenerateClients(l)
			engine, _, err := createEngine(l, engineClients)
			if err != nil {
				l.Error("Unable to create engine", "error", err)
				return
			}

			err = engine.Destroy()
			logger := createLogger()

			if err != nil {
				l.Error("Unable to destroy stack", "error", err)
				return
			}

			// clean up the data folders
			os.RemoveAll(utils.DataFolder("", os.ModePerm))
			os.RemoveAll(utils.LibraryFolder("", os.ModePerm))

			// shutdown ingress when we destroy all resources
			if cc.IsRunning() {
				err = cc.Stop()
				if err != nil {
					logger.Error("Unable to destroy jumppad daemon", "error", err)
				}
			}
		},
	}
}
