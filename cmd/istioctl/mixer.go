// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"istio.io/pilot/client/proxy"
	"istio.io/pilot/platform/kube"

	"github.com/ghodss/yaml"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"github.com/spf13/cobra"
)

// TODO This should come from something like istio.io/api instead of
// being hand copied from istio.io/mixer.
type mixerAPIResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Status rpc.Status  `json:"status,omitempty"`
}

const (
	requestTimeout = 60 * time.Second
	scopesPath     = "api/v1/scopes/"
)

var (
	mixerFile             string
	mixerFileContent      []byte
	istioGalleyAPIService string
	mixerRESTRequester    proxy.RESTRequester

	mixerCmd = &cobra.Command{
		Use:   "mixer",
		Short: "Istio Mixer configuration",
		Long: `
The Mixer configuration API allows users to configure all facets of the
Mixer.

See https://istio.io/docs/concepts/policy-and-control/mixer-config.html
for a description of Mixer configuration's scope, subject, and rules.
`,
		SilenceUsage: true,
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			var err error
			client, err = kubeClientFromConfig(kubeconfig)
			if err != nil {
				return err
			}

			if useKubeRequester {
				// TODO temporarily use namespace instead of
				// istioNamespace until istio/istio e2e tests are
				// updated.
				if istioNamespace == "" {
					istioNamespace = namespace
				}
				mixerRESTRequester = &k8sRESTRequester{
					client:    client,
					namespace: istioNamespace,
					service:   istioGalleyAPIService,
				}
			} else {
				mixerRESTRequester = &proxy.BasicHTTPRequester{
					BaseURL: istioGalleyAPIService,
					Client:  &http.Client{Timeout: requestTimeout},
					Version: kube.IstioResourceVersion,
				}
			}

			if c.Name() == "create" {
				if mixerFile == "" {
					return errors.New(c.UsageString())
				}
				data, err := ioutil.ReadFile(mixerFile)
				if err != nil {
					return fmt.Errorf("failed opening %s: %v", mixerFile, err)
				}
				mixerFileContent = data
			}

			return nil
		},
	}

	mixerRuleCmd = &cobra.Command{
		Use:   "rule",
		Short: "Istio Mixer Rule configuration",
		Long: `
Create and list Mixer rules in the configuration server.
`,
		SilenceUsage: true,
	}

	mixerRuleCreateCmd = &cobra.Command{
		Use:   "create <scope> <subject>",
		Short: "Create Istio Mixer rules",
		Example: `
# Create a new Mixer rule for the given scope and subject.
istioctl mixer rule create global myservice.ns.svc.cluster.local -f mixer-rule.yml
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			return mixerRuleCreate(args[0], args[1], mixerFileContent)
		},
	}
	mixerRuleGetCmd = &cobra.Command{
		Use:   "get <scope> <subject>",
		Short: "Get Istio Mixer rules",
		Long: `
Get Mixer rules for a given scope and subject.
`,
		Example: `
# Get the Mixer rule with scope='global' and subject='myservice.ns.svc.cluster.local'
istioctl mixer rule get global myservice.ns.svc.cluster.local
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			out, err := mixerRuleGet(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
	mixerRuleDeleteCmd = &cobra.Command{
		Use:   "delete <scope> <subject>",
		Short: "Delete Istio Mixer rules",
		Long: `
Delete Mixer rules for a given scope and subject.
`,
		Example: `
# Delete Mixer rules with scope='global' and subject='myservice.ns.svc.cluster.local'
istioctl mixer rule delete global myservice.ns.svc.cluster.local
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			return mixerRuleDelete(args[0], args[1])
		},
	}

	mixerAdapterCmd = &cobra.Command{
		Use:          "adapter",
		Short:        "Istio Mixer Adapter configuration",
		Long:         "Create and list Mixer adapters in the configuration server.",
		SilenceUsage: true,
	}

	mixerAdapterCreateCmd = &cobra.Command{
		Use:   "create <scope>",
		Short: "Create Istio Mixer adapters",
		Example: `
# Create new Mixer adapter configs for the given scope.
istioctl mixer adapter create global -f adapters.yml
`,
		RunE: mixerAdapterOrDescriptorCreateRunE,
	}

	mixerAdapterGetCmd = &cobra.Command{
		Use:   "get <scope>",
		Short: "Get Istio Mixer adapters",
		Example: `
# Get the Mixer adapter configs for the given scope.
istioctl mixer adapter get global
`,
		RunE: mixerAdapterOrDescriptorGetRunE,
	}

	mixerDescriptorCmd = &cobra.Command{
		Use:          "descriptor",
		Short:        "Istio Mixer Descriptor configuration",
		Long:         "Create and list Mixer descriptors in the configuration server.",
		SilenceUsage: true,
	}

	mixerDescriptorCreateCmd = &cobra.Command{
		Use:   "create <scope>",
		Short: "Create Istio Mixer descriptors",
		Example: `
# Create new Mixer descriptor configs for the given scope.
istioctl mixer descriptor create global -f adapters.yml
`,
		RunE: mixerAdapterOrDescriptorCreateRunE,
	}

	mixerDescriptorGetCmd = &cobra.Command{
		Use:   "get <scope>",
		Short: "Get Istio Mixer descriptors",
		Example: `
# Get the Mixer descriptor configs for the given scope.
istioctl mixer descriptor get global
`,
		RunE: mixerAdapterOrDescriptorGetRunE,
	}
)

func mixerGet(path string) (string, error) {
	status, body, err := mixerRESTRequester.Request(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", errors.New(http.StatusText(status))
	}

	response := map[string]interface{}{}
	if err = json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed processing response: %v", err)
	}
	data, err := yaml.Marshal(response["source_data"])
	if err != nil {
		return "", fmt.Errorf("failed formatting response: %v", err)
	}
	return string(data), nil
}

func mixerRequest(method, path string, reqBody []byte) error {
	status, respBody, err := mixerRESTRequester.Request(method, path, reqBody)

	// If we got output, let's look at it, even if we got an error.  The output might include the reason for the error.
	if respBody != nil {
		response := map[string]interface{}{}
		message := "unknown"
		fmt.Printf("%s\n", respBody)
		if errJSON := json.Unmarshal(respBody, &response); errJSON == nil {
			status := response["status"].(map[string]interface{})
			if msg, ok := status["message"]; ok {
				message = msg.(string)
			}
		}

		if status != http.StatusOK {
			return fmt.Errorf("failed to %s %s with status %v: %s", method, path, status, message)
		}

		fmt.Printf("%s\n", message)
	}

	return err
}

func mixerRulePath(scope, subject string) string {
	return fmt.Sprintf("core/rules/v1/%s/%s", url.PathEscape(scope), url.PathEscape(subject))
}

func mixerRuleCreate(scope, subject string, rule []byte) error {
	data := map[string]interface{}{}
	if err := yaml.Unmarshal(rule, &data); err != nil {
		return err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return mixerRequest(http.MethodPut, mixerRulePath(scope, subject), encoded)
}

func mixerRuleGet(scope, subject string) (string, error) {
	return mixerGet(mixerRulePath(scope, subject))
}

func mixerRuleDelete(scope, subject string) error {
	return mixerRequest(http.MethodDelete, mixerRulePath(scope, subject), nil)
}

func mixerAdapterOrDescriptorPath(scope, name string) string {
	return fmt.Sprintf("core/%s/v1//%s", url.PathEscape(name), url.PathEscape(scope))
}

func mixerAdapterOrDescriptorCreate(scope, name string, config []byte) error {
	path := mixerAdapterOrDescriptorPath(scope, name)
	data := map[string]interface{}{}
	if err := yaml.Unmarshal(config, &data); err != nil {
		return err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return mixerRequest(http.MethodPut, path, encoded)
}

func mixerAdapterOrDescriptorGet(scope, name string) (string, error) {
	path := mixerAdapterOrDescriptorPath(scope, name)
	return mixerGet(path)
}

func mixerAdapterOrDescriptorCreateRunE(c *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New(c.UsageString())
	}
	return mixerAdapterOrDescriptorCreate(args[0], c.Parent().Name()+"s", mixerFileContent)
}

func mixerAdapterOrDescriptorGetRunE(c *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New(c.UsageString())
	}
	out, err := mixerAdapterOrDescriptorGet(args[0], c.Parent().Name()+"s")
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func init() {
	mixerRuleCreateCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the Mixer rule")
	mixerAdapterCreateCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the adapters config")
	mixerDescriptorCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the descriptors config")
	mixerCmd.PersistentFlags().StringVar(&istioGalleyAPIService,
		"galleyAPIServer", "istio-galley:9096",
		"Name of istio-mixer service. When --kube=false this sets the address of the mixer service")

	mixerRuleCmd.AddCommand(mixerRuleCreateCmd)
	mixerRuleCmd.AddCommand(mixerRuleGetCmd)
	mixerRuleCmd.AddCommand(mixerRuleDeleteCmd)
	mixerCmd.AddCommand(mixerRuleCmd)
	mixerAdapterCmd.AddCommand(mixerAdapterCreateCmd)
	mixerAdapterCmd.AddCommand(mixerAdapterGetCmd)
	mixerCmd.AddCommand(mixerAdapterCmd)
	mixerDescriptorCmd.AddCommand(mixerDescriptorCreateCmd)
	mixerDescriptorCmd.AddCommand(mixerDescriptorGetCmd)
	mixerCmd.AddCommand(mixerDescriptorCmd)
	rootCmd.AddCommand(mixerCmd)
}
