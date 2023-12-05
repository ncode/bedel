/*
Copyright © 2023 Juliano Martinez <juliano@martinez.io>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"github.com/ncode/bedel/pkg/aclmanager"
	"github.com/spf13/viper"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// runOnceCmd represents the runOnce command
var runOnceCmd = &cobra.Command{
	Use:   "runOnce",
	Short: "Run the acl manager once, it will sync the follower with the primary",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		aclManager := aclmanager.New(viper.GetString("address"), viper.GetString("username"), viper.GetString("password"))
		function, err := aclManager.CurrentFunction(ctx)
		if err != nil {
			slog.Warn("unable to check if it's a Primary", "message", err)
			os.Exit(1)
		}
		if function == aclmanager.Follower {
			primary, err := aclManager.Primary(ctx)
			if err != nil {
				slog.Warn("unable to find Primary", "message", err)
				os.Exit(1)
			}
			var added, deleted []string
			added, deleted, err = aclManager.SyncAcls(ctx, primary)
			if err != nil {
				slog.Warn("unable to sync acls from Primary", "message", err)
				os.Exit(1)
			}
			slog.Info("Synced acls from Primary", "added", added, "deleted", deleted)
		} else {
			slog.Info("Not a follower, nothing to do")
		}
	},
}

func init() {
	rootCmd.AddCommand(runOnceCmd)
}
