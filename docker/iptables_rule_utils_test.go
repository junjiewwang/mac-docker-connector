package main

import "testing"

func TestIptablesLineSignatureMatchesDesiredRuleDespiteReordering(t *testing.T) {
	line := `-A FORWARD -s 172.21.0.0/16 -i br-b4ab88988d33 -o tun0 -m comment --comment "mdc-auto:route:172-21-0-0_16:bridge-to-tun" -j ACCEPT`
	rule := []string{"-i", "br-b4ab88988d33", "-s", "172.21.0.0/16", "-o", "tun0", "-j", "ACCEPT"}

	got := iptablesLineSignature(line)
	want := iptablesRuleSignature("FORWARD", rule)
	if got != want {
		t.Fatalf("signature mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestExtractManagedTagStripsQuotes(t *testing.T) {
	line := `-A FORWARD -i tun0 -o br-ef3f126c0103 -m comment --comment "mdc-auto:vmlink:host-k8s.service:192-168-49-0_24:tun-to-mk" -j ACCEPT`
	got := extractManagedTag(line)
	want := "mdc-auto:vmlink:host-k8s.service:192-168-49-0_24:tun-to-mk"
	if got != want {
		t.Fatalf("unexpected managed tag, want %q got %q", want, got)
	}
}

func TestInspectRuleDetectsManagedAndLegacySources(t *testing.T) {
	mgr := NewIptablesManager(false)
	mgr.cache["filter:FORWARD"] = "-A FORWARD -i tun0 -o br-test -j ACCEPT\n" +
		`-A FORWARD -o br-test -i tun0 -m comment --comment "mdc-auto:vmlink:host-docker:172-30-0-0_16:tun-to-bridge" -j ACCEPT`

	presence := mgr.InspectRule("filter", "FORWARD", []string{"-i", "tun0", "-o", "br-test", "-j", "ACCEPT"})
	if !presence.Managed {
		t.Fatalf("expected managed rule to be detected")
	}
	if !presence.Legacy {
		t.Fatalf("expected legacy rule to be detected")
	}
	if got := presence.Source(); got != "mixed" {
		t.Fatalf("unexpected source, want mixed got %q", got)
	}
}
