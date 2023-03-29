package controllers

import (
	"reflect"
	"testing"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getTime(h, m, s int) time.Time {
	return time.Date(2022, 02, 01, h, m, s, 0000, time.UTC)
}

func Test_NextSchedule(t *testing.T) {
	type args struct {
		module                 *tfaplv1beta1.Module
		now                    time.Time
		minIntervalBetweenRuns time.Duration
	}
	tests := []struct {
		name            string
		args            args
		wantMissedCount int
		wantNextSchd    time.Time
		wantErr         bool
	}{
		{
			name: "every_hour_initial_run",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 05, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "0 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    getTime(01, 15, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantMissedCount: 0, wantNextSchd: getTime(02, 00, 00), wantErr: false,
		},
		{
			name: "every_hour_initial_run_time_passed",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 05, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "0 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    getTime(02, 15, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantMissedCount: 1, wantNextSchd: getTime(03, 00, 00), wantErr: false,
		},
		{
			name: "every_hour_2nd_run",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 05, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "0 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: &metav1.Time{Time: getTime(02, 00, 01)}},
				},
				now:                    getTime(02, 15, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantMissedCount: 0, wantNextSchd: getTime(03, 00, 00), wantErr: false,
		},
		{
			name: "every_hour_2nd_run_time_passed",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 05, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "0 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: &metav1.Time{Time: getTime(02, 00, 01)}},
				},
				now:                    getTime(03, 15, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantMissedCount: 1, wantNextSchd: getTime(04, 00, 00), wantErr: false,
		},
		{
			name: "created_in_future!!",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(05, 05, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "0 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    getTime(01, 00, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantMissedCount: 0, wantNextSchd: getTime(02, 00, 00), wantErr: false,
		},
		{
			name: "logical_error_schedule_str",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 00, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "59 23 31 2 *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    getTime(02, 00, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantErr: true,
		},
		{
			name: "to_frequent_schedule",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: getTime(01, 00, 00)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "*/1 * * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    getTime(02, 00, 00),
				minIntervalBetweenRuns: time.Hour,
			},
			wantErr: true,
		},
		{
			name: "no_job_run_for_days",
			args: args{
				module: &tfaplv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)},
					},
					Spec:   tfaplv1beta1.ModuleSpec{Schedule: "00 */1 * * *"},
					Status: tfaplv1beta1.ModuleStatus{RunStartedAt: nil},
				},
				now:                    time.Date(2022, 03, 01, 01, 00, 00, 0000, time.UTC),
				minIntervalBetweenRuns: time.Hour,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMissedCount, gotNextSchd, err := NextSchedule(tt.args.module, tt.args.now, tt.args.minIntervalBetweenRuns)
			if (err != nil) != tt.wantErr {
				t.Errorf("NextSchedule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotMissedCount != tt.wantMissedCount {
				t.Errorf("NextSchedule() gotMissedCount = %v, wantMissedCount %v", gotMissedCount, tt.wantMissedCount)
			}
			if !reflect.DeepEqual(gotNextSchd, tt.wantNextSchd) {
				t.Errorf("NextSchedule() gotNextSchd = %v, wantNextSchd %v", gotNextSchd, tt.wantNextSchd)
			}
		})
	}
}
