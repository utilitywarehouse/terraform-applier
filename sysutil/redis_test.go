package sysutil

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestRedis_ParsePRRunsKey(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantPr     int
		wantHash   string
		wantErr    bool
	}{
		{"valid", args{"ns:name:PR:4:2ddi9df9d"}, types.NamespacedName{Namespace: "ns", Name: "name"}, 4, "2ddi9df9d", false},
		{"no-pr", args{"ns:name:lastRun"}, types.NamespacedName{}, 0, "", true},
		{"no-name", args{"ns:PR:4:2ddi9df9d"}, types.NamespacedName{}, 0, "", true},
		{"invalid-pr", args{"ns:name:PR:X:2ddi9df9d"}, types.NamespacedName{Namespace: "ns", Name: "name"}, 0, "2ddi9df9d", true},
		{"empty-values", args{"::PR:4:"}, types.NamespacedName{Namespace: "", Name: ""}, 4, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotPr, gotHash, err := ParsePRRunsKey(tt.args.str)
			if (err != nil) != tt.wantErr {
				t.Errorf("Redis.ParsePRRunsKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("Redis.ParsePRRunsKey() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if gotPr != tt.wantPr {
				t.Errorf("Redis.ParsePRRunsKey() gotPr = %v, want %v", gotPr, tt.wantPr)
			}
			if gotHash != tt.wantHash {
				t.Errorf("Redis.ParsePRRunsKey() gotHash = %v, want %v", gotHash, tt.wantHash)
			}
		})
	}
}
