package git

import "testing"

func TestUtil_RemoteURL(t *testing.T) {

	g := Util{
		Path: "../",
	}

	got, err := g.RemoteURL()
	if err != nil {
		t.Errorf("Util.RemoteURL() error = %v", err)
		return
	}

	want := "github.com/utilitywarehouse/terraform-applier"

	if got != want {
		t.Errorf("Util.RemoteURL() = %v, want %v", got, want)
	}
}
