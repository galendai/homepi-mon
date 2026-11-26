package scheduler_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/scheduler"
)

type openClashStub struct {
	*stubConnector
	report protocol.OpenClashReport
}

func (s *openClashStub) Provider() string { return "openclash" }
func (s *openClashStub) OpenClash() (protocol.OpenClashReport, error) {
	return s.report.Clone(), nil
}

func schedulerOpenClashReport(delay int) protocol.OpenClashReport {
	observedAt := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	return protocol.OpenClashReport{
		ObservedAt: observedAt, StaleAfter: protocol.Duration(15 * time.Second),
		DelayStaleAfter: protocol.Duration(100 * time.Second), SelectedGroup: "🚀 PROXY",
		SelectedNode: "node-a", Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{{
			Name: "node-a", Alive: new(true), DelayMS: new(delay), CheckedAt: &observedAt,
		}},
	}
}

func TestRefreshPublishesOpenClashAndPreservesLastKnownGood(t *testing.T) {
	store := newStore(t)
	base := &stubConnector{id: "clash"}
	reporter := &openClashStub{stubConnector: base, report: schedulerOpenClashReport(110)}
	base.set(nil, nil)
	s := scheduler.New([]scheduler.Task{{
		Connector: reporter, Enabled: true, StaleAfter: 20 * time.Second,
	}}, scheduler.Options{Store: store, Logger: quietLogger()})
	if err := s.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	report := store.Snapshot().OpenClash
	if report == nil || *report.Nodes[0].DelayMS != 110 || report.StaleAfter.D() != 20*time.Second {
		t.Fatalf("OpenClash report = %+v", report)
	}
	base.set(nil, connector.Errorf(protocol.ErrTimeout, "controller timed out"))
	if err := s.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	report = store.Snapshot().OpenClash
	if *report.Nodes[0].DelayMS != 110 || report.ErrorClass != protocol.ErrTimeout ||
		report.Message != "controller timed out" {
		t.Fatalf("last-known-good report = %+v", report)
	}
}

func TestOpenClashAuthKeepsKeyringGuidance(t *testing.T) {
	store := newStore(t)
	base := &stubConnector{id: "clash"}
	reporter := &openClashStub{stubConnector: base, report: schedulerOpenClashReport(110)}
	base.set(nil, connector.Errorf(protocol.ErrAuth, "authentication rejected; update the keyring credential"))
	s := scheduler.New([]scheduler.Task{{Connector: reporter, Enabled: true}},
		scheduler.Options{Store: store, Logger: quietLogger(), NodeLabel: "ROUTER"})
	if err := s.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	health := store.Snapshot().ConnectorHealth
	if len(health) != 1 || health[0].Message != "authentication rejected; update the keyring credential" {
		t.Fatalf("health = %+v", health)
	}
	if strings.Contains(health[0].Message, "official CLI") {
		t.Fatal("OpenClash auth incorrectly recommends the official CLI")
	}
}
