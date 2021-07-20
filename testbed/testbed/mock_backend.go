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

package testbed

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"go.uber.org/atomic"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
)

// MockBackend is a backend that allows receiving the data locally.
type MockBackend struct {
	// Metric and trace consumers
	tc *MockTraceConsumer
	mc *MockMetricConsumer
	lc *MockLogConsumer

	receiver DataReceiver

	// Log file
	logFilePath string
	logFile     *os.File

	// Start/stop flags
	isStarted bool
	stopOnce  sync.Once
	startedAt time.Time

	// Recording fields.
	isRecording                 bool
	isRecordingMetricTimestamps bool
	recordMutex                 sync.Mutex
	ReceivedTraces              []pdata.Traces
	ReceivedMetrics             []pdata.Metrics
	ReceivedLogs                []pdata.Logs
	ReceivedTimestamps          []time.Time
}

// NewMockBackend creates a new mock backend that receives data using specified receiver.
func NewMockBackend(logFilePath string, receiver DataReceiver) *MockBackend {
	mb := &MockBackend{
		logFilePath: logFilePath,
		receiver:    receiver,
		tc:          &MockTraceConsumer{},
		mc:          &MockMetricConsumer{},
		lc:          &MockLogConsumer{},
	}
	mb.tc.backend = mb
	mb.mc.backend = mb
	mb.lc.backend = mb
	return mb
}

// Start a backend.
func (mb *MockBackend) Start() error {
	log.Printf("Starting mock backend...")

	var err error

	// Open log file
	mb.logFile, err = os.Create(mb.logFilePath)
	if err != nil {
		return err
	}

	err = mb.receiver.Start(mb.tc, mb.mc, mb.lc)
	if err != nil {
		return err
	}

	mb.isStarted = true
	mb.startedAt = time.Now()
	return nil
}

// Stop the backend
func (mb *MockBackend) Stop() {
	mb.stopOnce.Do(func() {
		if !mb.isStarted {
			return
		}

		log.Printf("Stopping mock backend...")

		mb.logFile.Close()
		if err := mb.receiver.Stop(); err != nil {
			log.Printf("Failed to stop receiver: %v", err)
		}
		// Print stats.
		log.Printf("Stopped backend. %s", mb.GetStats())
	})
}

// EnableRecording enables recording of all data received by MockBackend.
func (mb *MockBackend) EnableRecording() {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	mb.isRecording = true
}

// EnableMetricTimestampRecording enables recording of metric timestamps by MockBackend.
func (mb *MockBackend) EnableMetricTimestampRecording() {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	mb.isRecordingMetricTimestamps = true
}

func (mb *MockBackend) GetStats() string {
	received := mb.DataItemsReceived()
	return printer.Sprintf("Received:%10d items (%d/sec)", received, int(float64(received)/time.Since(mb.startedAt).Seconds()))
}

// DataItemsReceived returns total number of received spans and metrics.
func (mb *MockBackend) DataItemsReceived() uint64 {
	return mb.tc.numSpansReceived.Load() + mb.mc.numMetricsReceived.Load() + mb.lc.numLogRecordsReceived.Load()
}

// ClearReceivedItems clears the list of received traces and metrics. Note: counters
// return by DataItemsReceived() are not cleared, they are cumulative.
func (mb *MockBackend) ClearReceivedItems() {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	mb.ReceivedTraces = nil
	mb.ReceivedMetrics = nil
	mb.ReceivedLogs = nil
	mb.ReceivedTimestamps = nil
}

func (mb *MockBackend) ConsumeTrace(td pdata.Traces) {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	if mb.isRecording {
		mb.ReceivedTraces = append(mb.ReceivedTraces, td)
	}
}

func (mb *MockBackend) ConsumeMetric(md pdata.Metrics) {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	if mb.isRecording {
		mb.ReceivedMetrics = append(mb.ReceivedMetrics, md)
	}

	// Record the timestamp of a metric from each scrape.
	// Then the validator can check the difference between timestamps are the same as the scrape interval.
	if mb.isRecordingMetricTimestamps {
		currentTimestamp := getFirstMetricTimestamp(md)
		receivedTimestampsCount := len(mb.ReceivedTimestamps)

		// Record the timestamp only if it's the first recorded or if it's different from the last recorded timestamp
		if receivedTimestampsCount == 0 || !mb.ReceivedTimestamps[receivedTimestampsCount-1].Equal(currentTimestamp) {
			mb.ReceivedTimestamps = append(mb.ReceivedTimestamps, currentTimestamp)
		}
	}
}

// Get the timestamp of the first metric data point.
// This can be used for load testing with scraping to ensure the scraper can keep up with the scrape interval.
func getFirstMetricTimestamp(md pdata.Metrics) time.Time {
	currentTimestamp := time.Time{}
	rms := md.ResourceMetrics()
	if rms.Len() > 0 {
		rm := rms.At(0)
		ilms := rm.InstrumentationLibraryMetrics()
		if ilms.Len() > 0 {
			ilm := ilms.At(0)
			ms := ilm.Metrics()
			if ms.Len() > 0 {
				m := ms.At(0)
				switch m.DataType() {
				case pdata.MetricDataTypeIntGauge:
					dataPoints := m.IntGauge().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				case pdata.MetricDataTypeGauge:
					dataPoints := m.Gauge().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				case pdata.MetricDataTypeIntSum:
					dataPoints := m.IntSum().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				case pdata.MetricDataTypeSum:
					dataPoints := m.Sum().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				case pdata.MetricDataTypeHistogram:
					dataPoints := m.Histogram().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				case pdata.MetricDataTypeSummary:
					dataPoints := m.Summary().DataPoints()
					if dataPoints.Len() > 0 {
						currentTimestamp = dataPoints.At(0).Timestamp().AsTime()
					}
				}
			}
		}
	}
	return currentTimestamp
}

var _ consumer.Traces = (*MockTraceConsumer)(nil)

func (mb *MockBackend) ConsumeLogs(ld pdata.Logs) {
	mb.recordMutex.Lock()
	defer mb.recordMutex.Unlock()
	if mb.isRecording {
		mb.ReceivedLogs = append(mb.ReceivedLogs, ld)
	}
}

type MockTraceConsumer struct {
	numSpansReceived atomic.Uint64
	backend          *MockBackend
}

func (tc *MockTraceConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (tc *MockTraceConsumer) ConsumeTraces(_ context.Context, td pdata.Traces) error {
	tc.numSpansReceived.Add(uint64(td.SpanCount()))

	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		ils := rs.At(i).InstrumentationLibrarySpans()
		for j := 0; j < ils.Len(); j++ {
			spans := ils.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				var spanSeqnum int64
				var traceSeqnum int64

				seqnumAttr, ok := span.Attributes().Get("load_generator.span_seq_num")
				if ok {
					spanSeqnum = seqnumAttr.IntVal()
				}

				seqnumAttr, ok = span.Attributes().Get("load_generator.trace_seq_num")
				if ok {
					traceSeqnum = seqnumAttr.IntVal()
				}

				// Ignore the seqnums for now. We will use them later.
				_ = spanSeqnum
				_ = traceSeqnum

			}
		}
	}

	tc.backend.ConsumeTrace(td)

	return nil
}

var _ consumer.Metrics = (*MockMetricConsumer)(nil)

type MockMetricConsumer struct {
	numMetricsReceived atomic.Uint64
	backend            *MockBackend
}

func (mc *MockMetricConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (mc *MockMetricConsumer) ConsumeMetrics(_ context.Context, md pdata.Metrics) error {
	mc.numMetricsReceived.Add(uint64(md.DataPointCount()))
	mc.backend.ConsumeMetric(md)
	return nil
}

func (tc *MockTraceConsumer) MockConsumeTraceData(spanCount int) error {
	tc.numSpansReceived.Add(uint64(spanCount))
	return nil
}

func (mc *MockMetricConsumer) MockConsumeMetricData(metricsCount int) error {
	mc.numMetricsReceived.Add(uint64(metricsCount))
	return nil
}

type MockLogConsumer struct {
	numLogRecordsReceived atomic.Uint64
	backend               *MockBackend
}

func (lc *MockLogConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (lc *MockLogConsumer) ConsumeLogs(_ context.Context, ld pdata.Logs) error {
	recordCount := ld.LogRecordCount()
	lc.numLogRecordsReceived.Add(uint64(recordCount))
	lc.backend.ConsumeLogs(ld)
	return nil
}
