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
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ncode/bedel/pkg/aclmanager"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the acl manager in mood loop, it will sync the follower with the primary",
	Run: func(cmd *cobra.Command, args []string) {
		mgr := aclmanager.New(viper.GetString("address"), viper.GetString("username"), viper.GetString("password"), viper.GetBool("aclfile"))
		defer mgr.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Set up signal handling
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			cancel()
		}()

		syncInterval := viper.GetDuration("syncInterval")
		logger.Info("Starting ACL manager loop")
		err := mgr.Loop(ctx, syncInterval)
		if err != nil {
			logger.Error("Error running ACL manager loop", "error", err)
			os.Exit(1)
		}
		logger.Info("ACL manager loop terminated")
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
