package analytics

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/kubeshop/botkube/pkg/config"
	"github.com/kubeshop/botkube/pkg/execute/command"
)

var _ Reporter = &NoopReporter{}

// NoopReporter implements Reporter interface, but is a no-op (doesn't execute any logic).
type NoopReporter struct{}

// NewNoopReporter creates new NoopReporter instance.
func NewNoopReporter() *NoopReporter {
	return &NoopReporter{}
}

// RegisterCurrentIdentity loads the current anonymous identity and registers it.
func (n NoopReporter) RegisterCurrentIdentity(_ context.Context, _ kubernetes.Interface, _ string) error {
	return nil
}

// ReportCommand reports a new executed command. The command should be anonymized before using this method.
func (n NoopReporter) ReportCommand(_ config.CommPlatformIntegration, _ string, _ command.Origin, _ bool) error {
	return nil
}

// ReportBotEnabled reports an enabled bot.
func (n NoopReporter) ReportBotEnabled(_ config.CommPlatformIntegration) error {
	return nil
}

// ReportSinkEnabled reports an enabled sink.
func (n NoopReporter) ReportSinkEnabled(_ config.CommPlatformIntegration) error {
	return nil
}

// ReportHandledEventSuccess reports a successfully handled event using a given communication platform.
func (n NoopReporter) ReportHandledEventSuccess(_ ReportEvent) error {
	return nil
}

// ReportHandledEventError reports a failure while handling event using a given communication platform.
func (n NoopReporter) ReportHandledEventError(_ ReportEvent, _ error) error {
	return nil
}

// ReportFatalError reports a fatal app error.
func (n NoopReporter) ReportFatalError(_ error) error {
	return nil
}

// Close cleans up the reporter resources.
func (n NoopReporter) Close() error {
	return nil
}
