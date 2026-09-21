package main

import "testing"

// TestWhatToRunOnTheOtherMachine is herdr's choice when attaching remotely:
// the far side's tend of this very build wherever it is; else install this
// one when the machines are alike; else the far side's own with a warning;
// else refuse. If it regresses, a remote session runs a build that
// disagrees with this client, or none.
func TestWhatToRunOnTheOtherMachine(t *testing.T) {
	probe := parseProbe("Linux x86_64\ngzip\ntend\t/usr/local/bin/tend\ttend old1\ntend\t/root/.local/bin/tend\ttend new2\n")
	if probe.Platform != "Linux x86_64" || !probe.Gzip || len(probe.Tends) != 2 || probe.Tends[1].Version != "new2" {
		t.Fatalf("probe = %+v", probe)
	}
	if plan, err := planRemote(probe, "new2", "Linux x86_64"); err != nil || plan.use != "/root/.local/bin/tend" || plan.install {
		t.Errorf("a matching build should be used where it is: %+v %v", plan, err)
	}
	if plan, _ := planRemote(probe, "new3", "Linux x86_64"); !plan.install {
		t.Errorf("no match on a like machine should install: %+v", plan)
	}
	if plan, err := planRemote(probe, "new3", "Darwin arm64"); err != nil || plan.use != "/usr/local/bin/tend" || plan.warning == "" {
		t.Errorf("unlike machine should use what is there, warning: %+v %v", plan, err)
	}
	if _, err := planRemote(parseProbe("Linux aarch64\n"), "new3", "Linux x86_64"); err == nil {
		t.Error("nothing there and nothing to copy should be refused")
	}
}
