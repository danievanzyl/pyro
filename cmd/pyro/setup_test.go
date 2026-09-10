package main

import (
	"strings"
	"testing"

	pyro "github.com/danievanzyl/pyro"
)

func TestRenderServiceUnit_DefaultBaseMatchesEmbedded(t *testing.T) {
	got := renderServiceUnit("/opt/pyro")
	want := string(pyro.ServiceUnit)
	if got != want {
		t.Errorf("renderServiceUnit(\"/opt/pyro\") must be byte-identical to the embedded unit\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderServiceUnit_CustomBaseSubstitutesEveryOptPyroPath(t *testing.T) {
	got := renderServiceUnit("/srv/pyro")
	if strings.Contains(got, "/opt/pyro") {
		t.Errorf("renderServiceUnit(\"/srv/pyro\") still references /opt/pyro:\n%s", got)
	}
	if !strings.Contains(got, "ExecStart=/srv/pyro/bin/pyro-server") {
		t.Errorf("renderServiceUnit(\"/srv/pyro\") missing substituted ExecStart:\n%s", got)
	}
	if !strings.Contains(got, "WorkingDirectory=/srv/pyro") {
		t.Errorf("renderServiceUnit(\"/srv/pyro\") missing substituted WorkingDirectory:\n%s", got)
	}
}

func TestRenderServiceUnit_NoLegacyFirecrackerFlag(t *testing.T) {
	got := renderServiceUnit("/opt/pyro")
	if strings.Contains(got, "--firecracker") {
		t.Errorf("unit must not pin --firecracker, the compiled default (/usr/bin/firecracker) already matches most distro packages:\n%s", got)
	}
}

func TestRenderServiceUnit_PoolSizeNotOne(t *testing.T) {
	got := renderServiceUnit("/opt/pyro")
	if strings.Contains(got, "--pool-size 1") {
		t.Errorf("unit must not set --pool-size 1, Pool.Claim has no callers:\n%s", got)
	}
}

func TestRenderServiceUnit_BridgeCreatedBeforeStart(t *testing.T) {
	got := renderServiceUnit("/opt/pyro")
	preIdx := strings.Index(got, "ExecStartPre=")
	startIdx := strings.Index(got, "\nExecStart=")
	if preIdx == -1 {
		t.Fatalf("unit missing ExecStartPre bridge setup:\n%s", got)
	}
	if startIdx == -1 || preIdx > startIdx {
		t.Errorf("ExecStartPre must precede ExecStart so bridge failure blocks server start:\n%s", got)
	}
}
