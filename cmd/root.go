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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "bedel",
	Short: "Small utility to sync redis acls with a master instance",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.bedel.yaml)")
	rootCmd.PersistentFlags().StringP("address", "a", "", "address of the slave to manage instance eg: 127.0.0.1:6379")
	viper.BindPFlag("address", rootCmd.PersistentFlags().Lookup("address"))
	rootCmd.PersistentFlags().StringP("password", "p", "", "password to manage acls")
	viper.BindPFlag("password", rootCmd.PersistentFlags().Lookup("password"))
	rootCmd.PersistentFlags().StringP("username", "u", "", "username to manage acls")
	viper.BindPFlag("username", rootCmd.PersistentFlags().Lookup("username"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".bedel" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".bedel")
	}

	viper.SetDefault("syncInterval", 10)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	if !viper.IsSet("address") {
		fmt.Fprintln(os.Stderr, "address is required")
		os.Exit(1)
	}

	if !viper.IsSet("password") {
		fmt.Fprintln(os.Stderr, "password is required")
		os.Exit(1)
	}

	if !viper.IsSet("username") {
		fmt.Fprintln(os.Stderr, "username is required")
		os.Exit(1)
	}
}
