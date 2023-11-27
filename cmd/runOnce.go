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

	"github.com/spf13/cobra"
)

// runOnceCmd represents the runOnce command
var runOnceCmd = &cobra.Command{
	Use:   "runOnce",
	Short: "Run the acl manager once, it will sync the slave with the master",
	Run: func(cmd *cobra.Command, args []string) {
		mgr := aclmanager.New(viper.GetString("address"), viper.GetString("password"), viper.GetString("password"))
		err := mgr.SyncAcls()
		if err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(runOnceCmd)
}
