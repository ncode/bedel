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
	"log/slog"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var lvl = new(slog.LevelVar)
var logger = slog.New(
	slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level:     lvl,
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.SourceKey {
					s := a.Value.Any().(*slog.Source)
					s.File = path.Base(s.File)
				}
				return a
			},
		},
	),
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "bedel",
	Short: "Small utility to sync redis acls with a primary instance",
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
	rootCmd.PersistentFlags().StringP("address", "a", "", "address of the instance to manage, e.g., 127.0.0.1:6379")
	viper.BindPFlag("address", rootCmd.PersistentFlags().Lookup("address"))
	rootCmd.PersistentFlags().StringP("password", "p", "", "password to manage acls")
	viper.BindPFlag("password", rootCmd.PersistentFlags().Lookup("password"))
	rootCmd.PersistentFlags().StringP("username", "u", "", "username to manage acls")
	viper.BindPFlag("username", rootCmd.PersistentFlags().Lookup("username"))
	rootCmd.PersistentFlags().StringP("logLevel", "l", "INFO", "set default logLevel")
	viper.BindPFlag("logLevel", rootCmd.PersistentFlags().Lookup("logLevel"))
	rootCmd.PersistentFlags().Bool("aclfile", false, "defined if we should use the aclfile to sync acls")
	viper.BindPFlag("aclfile", rootCmd.PersistentFlags().Lookup("aclfile"))
	rootCmd.PersistentFlags().Duration("syncInterval", 10*time.Second, "interval between sync operations")
	viper.BindPFlag("syncInterval", rootCmd.PersistentFlags().Lookup("syncInterval"))
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
	viper.SetDefault("username", "default")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	if !viper.IsSet("address") {
		logger.Error("Address is required")
		os.Exit(1)
	}

	if !viper.IsSet("password") {
		logger.Error("password is required")
		os.Exit(1)
	}

	if !viper.IsSet("username") {
		viper.SetDefault("username", "default")
	}

	err := lvl.UnmarshalText([]byte(viper.GetString("logLevel")))
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	slog.SetDefault(logger)
}
