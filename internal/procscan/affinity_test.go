package procscan

import "testing"

func TestAllowedCPUsFromStatus(t *testing.T) {
	status := "" +
		"Name:\tfoo\n" +
		"Uid:\t1000\t1000\t1000\t1000\n" +
		"Cpus_allowed_list:\t0-3,8-11\n"

	got, ok := allowedCPUsFromStatus([]byte(status))
	if !ok {
		t.Fatalf("expected ok")
	}
	if got != "0-3,8-11" {
		t.Fatalf("unexpected allowed cpus: %q", got)
	}
}

func TestAllowedCPUsFromStatusMissing(t *testing.T) {
	status := "Name:\tfoo\nUid:\t1000\t1000\t1000\t1000\n"
	_, ok := allowedCPUsFromStatus([]byte(status))
	if ok {
		t.Fatalf("expected missing")
	}
}
