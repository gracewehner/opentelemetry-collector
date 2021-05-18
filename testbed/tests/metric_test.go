// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tests

// This file contains Test functions which initiate the tests. The tests can be either
// coded in this file or use scenarios from perf_scenarios.go.

import (
	"log"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/collector/testbed/testbed"
)

var (
	scrapeIntervalEnvVar = "SCRAPE_INTERVAL"
)

func TestMetricNoBackend10kDPSOpenCensus(t *testing.T) {
	options := testbed.LoadOptions{DataItemsPerInterval: 10_000, ItemsPerBatch: 10}
	dataProvider := testbed.NewPerfTestDataProvider(options)
	tc := testbed.NewTestCase(
		t,
		dataProvider,
		testbed.NewOCMetricDataSender(testbed.DefaultHost, 55678),
		testbed.NewOCDataReceiver(testbed.DefaultOCPort),
		&testbed.ChildProcess{},
		&testbed.PerfTestValidator{},
		performanceResultsSummary,
	)
	defer tc.Stop()

	tc.SetResourceLimits(testbed.ResourceSpec{ExpectedMaxCPU: 200, ExpectedMaxRAM: 200})
	tc.StartAgent()

	tc.StartLoad(testbed.LoadOptions{DataItemsPerInterval: 10_000})

	tc.Sleep(tc.Duration)
}

func TestMetric10kDPS(t *testing.T) {
	tests := []struct {
		name         string
		sender       testbed.DataSender
		receiver     testbed.DataReceiver
		resourceSpec testbed.ResourceSpec
	}{
		{
			"OpenCensus",
			testbed.NewOCMetricDataSender(testbed.DefaultHost, testbed.GetAvailablePort(t)),
			testbed.NewOCDataReceiver(testbed.GetAvailablePort(t)),
			testbed.ResourceSpec{
				ExpectedMaxCPU: 85,
				ExpectedMaxRAM: 75,
			},
		},
		{
			"OTLP",
			testbed.NewOTLPMetricDataSender(testbed.DefaultHost, testbed.GetAvailablePort(t)),
			testbed.NewOTLPDataReceiver(testbed.GetAvailablePort(t)),
			testbed.ResourceSpec{
				ExpectedMaxCPU: 50,
				ExpectedMaxRAM: 65,
			},
		},
		{
			"OTLP-HTTP",
			testbed.NewOTLPHTTPMetricDataSender(testbed.DefaultHost, testbed.GetAvailablePort(t)),
			testbed.NewOTLPHTTPDataReceiver(testbed.GetAvailablePort(t)),
			testbed.ResourceSpec{
				ExpectedMaxCPU: 50,
				ExpectedMaxRAM: 65,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Scenario10kItemsPerSecond(
				t,
				test.sender,
				test.receiver,
				test.resourceSpec,
				performanceResultsSummary,
				nil,
				nil,
			)
		})
	}

}

func TestMetrics10kDPSScraped(t *testing.T) {
	scrapeIntervalString := os.Getenv(scrapeIntervalEnvVar)
	if scrapeIntervalString == "" {
		scrapeIntervalString = "1s"
	}
	scrapeInterval, err := time.ParseDuration(scrapeIntervalString)
	if err != nil {
		log.Fatalf("Invalid "+scrapeIntervalEnvVar+": %v. Expecting a valid duration string.", scrapeInterval)
	}
	tests := []struct {
		name           string
		sender         testbed.DataSender
		receiver       testbed.DataReceiver
		resourceSpec   testbed.ResourceSpec
		processors     map[string]string
		scrapeInterval time.Duration
	}{
		{
			"PrometheusReceiver-OTLPExporter",
			testbed.NewPrometheusDataSender(testbed.DefaultHost, testbed.GetAvailablePort(t), scrapeInterval.String()),
			testbed.NewOTLPDataReceiver(testbed.GetAvailablePort(t)),
			testbed.ResourceSpec{
				ExpectedMaxCPU: 15,
				ExpectedMaxRAM: 85,
			},
			map[string]string{
				"batch": `batch:
    send_batch_max_size: 5000
`,
			},
			scrapeInterval,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Scenario10kScrapeItemsPerSecond(
				t,
				test.sender,
				test.receiver,
				test.resourceSpec,
				performanceResultsSummary,
				test.processors,
				nil,
				test.scrapeInterval,
			)
		})
	}
}
