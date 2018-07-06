// Package main provides the command line based tool for the Landoop's Lenses client REST API.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/landoop/lenses-go"

	"github.com/landoop/bite"
	"github.com/spf13/cobra"
)

var (
	// buildRevision is the build revision (docker commit string) but it's
	// available only on the build state, on the cli executable - via the "version" command.
	buildRevision = ""
	// buildTime is the build unix time (in nanoseconds), like the `buildRevision`,
	// this is available on after the build state, inside the cli executable - via the "version" command.
	//
	// Note that this BuildTime is not int64, it's type of string.
	buildTime = ""
)

var (
	client *lenses.Client
)

const examplePrefix = `lenses-cli %s`

func exampleString(str string) string {
	return fmt.Sprintf(examplePrefix, str)
}

var rootCmd = &cobra.Command{
	Use:                        "lenses-cli [command] [flags]",
	Example:                    exampleString(`sql --offsets --stats=2s "SELECT * FROM reddit_posts LIMIT 50"`),
	Short:                      "Lenses-cli is the command line client for the Landoop's Lenses REST API.",
	Version:                    lenses.Version,
	SilenceUsage:               true,
	Long:                       "lenses-cli - manage Lenses resources and developer workflow",
	SilenceErrors:              true,
	TraverseChildren:           true,
	SuggestionsMinimumDistance: 1,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		// check for old config, if found then convert to its new format before anything else.
		// if err := configManager.applyCompatibility(); err != nil {
		// 	return err
		// }

		ok, err := configManager.load()
		// if command is "configure" and the configuration is invalid at this point, don't give a failure,
		// let the configure command give a tutorial for user in order to create a configuration file.
		// Note that if clientConfig is valid and we are inside the configure command
		// then the configure will normally continue and save the valid configuration (that normally came from flags).
		if name := cmd.Name(); name == "configure" || name == "context" || name == "contexts" {
			return nil
		}

		// it's not nil, if context does not exist then it would throw an error.
		currentConfig := configManager.config.GetCurrent()
		for !ok {
			if err != nil {
				return err
			}

			if currentConfig.Debug {
				fmt.Fprintf(cmd.OutOrStdout(), "%#+v\n", *currentConfig)
			}

			fmt.Fprintln(cmd.OutOrStderr(), "cannot retrieve credentials, please configure below")
			configureCmd := newConfigureCommand()
			// disable any flags passed on the parent command before execute.
			configureCmd.DisableFlagParsing = true
			if err = configureCmd.Execute(); err != nil {
				return err
			}

			ok, err = configManager.load()
		}

		// if login, remove the token so setupClient will generate a new one and save it to the home dir/lenses-cli.yml.
		if cmd.Name() == "login" {
			currentConfig.Token = ""

			if basicAuth, isBasicAuth := currentConfig.Authentication.(lenses.BasicAuthentication); isBasicAuth {
				//  and fire any errors if host or user or pass are not there.
				if currentConfig.Host == "" || basicAuth.Username == "" || basicAuth.Password == "" {
					// return fmt.Errorf("cannot retrieve credentials, please setup the configuration using the '%s' command first", "configure")
					//
					if err := newConfigureCommand().Execute(); err != nil {
						return err
					}

					// add a new line, so the login's session welcome messages has its place.
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}

			return nil
		}

		// if config.Debug {
		// 	cmd.DebugFlags()
		// }

		// don't connect to the HTTP REST API when command is "live" (websocket).
		if cmd.Name() == "live" {
			return
		}

		return setupClient()
	},
}

func setupClient() (err error) {
	client, err = lenses.OpenConnection(*configManager.config.GetCurrent())
	return
}

// timeLayout defines the datetime layout for the `buildTime`.
const timeLayout = time.UnixDate

func buildVersionTmpl() string {
	/*
		- lenses-cli --version
		- version is the semantic version of the client package itself.
		- "build revision" is the build revision, available on build state, on the cli executable itself.
		- "build datetime" is originally the build time in unix nano seconds, formatted to human-readable text.
		- Output format:
			lenses-cli version 2.0
			>>>> build
						revision 27c7532fc6bf9c02bc7cf4575036ba0011f4c09a
						datetime Tu April 03 07:09:42 UTC 2018
						go       1.10
	*/
	buildTitle := ">>>> build" // if we ever want an emoji, there is one: \U0001f4bb
	tab := strings.Repeat(" ", len(buildTitle))

	// unix nanoseconds, as int64, to a human readable time, defaults to time.UnixDate, i.e:
	// Thu Mar 22 02:40:53 UTC 2018
	// but can be changed to something like "Mon, 01 Jan 2006 15:04:05 GMT" if needed.
	n, _ := strconv.ParseInt(buildTime, 10, 64)
	buildTimeStr := time.Unix(n, 0).Format(timeLayout)

	return `{{with .Name}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}` +
		fmt.Sprintf("\n%s\n", buildTitle) +
		fmt.Sprintf("%s revision %s\n", tab, buildRevision) +
		fmt.Sprintf("%s datetime %s\n", tab, buildTimeStr) +
		fmt.Sprintf("%s go       %s\n", tab, runtime.Version())
}

var (
	errResourceNotFoundMessage      string
	errResourceNotAccessibleMessage string
	errResourceNotGoodMessage       string
)

type errorMap map[int]string

func mapError(err error, messages errorMap) error {
	if err == nil {
		return nil
	}

	// catch any errors that should be described by the command that gave that error.
	if resourceErr, ok := err.(lenses.ResourceError); ok {
		if messages != nil {
			if errMsg, ok := messages[resourceErr.Code()]; ok {
				return errors.New(errMsg)
			}
		}
	}

	return err
}

var configManager *configurationManager

func main() {
	rootCmd.SetVersionTemplate(buildVersionTmpl())
	bite.RegisterMachineFriendlyFlagTo(rootCmd.PersistentFlags(), nil)

	configManager = newConfigurationManager(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		// catch any errors that should be described by the command that gave that error.
		// each errResourceXXXMessage should be declared inside the command,
		// they are global variables and that's because we don't want to get dirdy on each resource command, don't change it unless discussion.
		err = mapError(err, errorMap{
			404: errResourceNotFoundMessage,
			403: errResourceNotAccessibleMessage,
			400: errResourceNotGoodMessage,
		})

		// always new line because of the unix terminal.
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
