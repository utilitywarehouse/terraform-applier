package prplanner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/redis/go-redis/v9"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func mustParseTime(str string) *time.Time {
	at, err := time.Parse(time.RFC3339, str)
	if err != nil {
		panic(err)
	}
	return &at
}

func mustParseMetaTime(str string) *metav1.Time {
	at, err := time.Parse(time.RFC3339, str)
	if err != nil {
		panic(err)
	}
	return &metav1.Time{Time: at}
}

func Test_processRedisKeySetMsg(t *testing.T) {
	ctx := context.Background()
	err := tfaplv1beta1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	kubeClient := fake.NewFakeClient(
		&tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/tfaplv1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "admins", Namespace: "foo"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/utilitywarehouse/terraform-applier.git",
				Path:    "foo/admins",
			},
		},
		&tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/tfaplv1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "users", Namespace: "foo",
			},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "git@github.com:utilitywarehouse/terraform-applier.git",
				Path:    "foo/users",
			},
		},
	)

	goMockCtrl := gomock.NewController(t)
	testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
	testGithub := NewMockGithubInterface(goMockCtrl)
	planner := &Planner{
		Log:         slog.Default(),
		RedisClient: testRedis,
		ClusterClt:  kubeClient,
		github:      testGithub,
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)

	ch := make(chan *redis.Message)
	defer close(ch)

	go planner.processRedisKeySetMsg(ctx, ch)
	time.Sleep(time.Second)

	t.Run("valid PR key updated", func(t *testing.T) {
		key := "foo:admins:PR:4:d91f6ff"

		testRedis.EXPECT().Run(gomock.Any(), key).
			Return(&tfaplv1beta1.Run{Module: types.NamespacedName{Namespace: "foo", Name: "admins"}, Request: &tfaplv1beta1.Request{PR: &tfaplv1beta1.PullRequest{Number: 4, CommentID: 123}}, CommitHash: "hash1", Output: "terraform plan output"}, nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment("utilitywarehouse", "terraform-applier", 123, 4, gomock.Any()).
			Return(123, nil)

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("valid PR key updated module2", func(t *testing.T) {
		key := "foo:users:PR:4:d91f6ff"

		testRedis.EXPECT().Run(gomock.Any(), key).
			Return(&tfaplv1beta1.Run{Module: types.NamespacedName{Namespace: "foo", Name: "users"}, Request: &tfaplv1beta1.Request{PR: &tfaplv1beta1.PullRequest{Number: 4, CommentID: 123}}, CommitHash: "hash1", Output: "terraform plan output"}, nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment("utilitywarehouse", "terraform-applier", 123, 4, gomock.Any()).
			Return(123, nil)

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("valid apply key", func(t *testing.T) {
		key := "foo:admins:default:lastApply"

		testRedis.EXPECT().Run(gomock.Any(), key).
			Return(&tfaplv1beta1.Run{Module: types.NamespacedName{Namespace: "foo", Name: "admins"}, Applied: true, Request: &tfaplv1beta1.Request{}, CommitHash: "hash1", CommitMsg: "some commit msg... (#4)", Output: "terraform apply output"}, nil)

		testRedis.EXPECT().Runs(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "admins"}, "PR:4:*").
			Return([]*tfaplv1beta1.Run{{Module: types.NamespacedName{Namespace: "foo", Name: "admins"}, Request: &tfaplv1beta1.Request{PR: &tfaplv1beta1.PullRequest{Number: 4, CommentID: 123}}, CommitHash: "hash1", Output: "terraform plan output"}}, nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment("utilitywarehouse", "terraform-applier", 0, 4, gomock.Any()).
			Return(123, nil)

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("invalid channel", func(t *testing.T) {
		key := "foo:admins:PR:4:d91f6ff"
		// not expecting any other calls
		// hence no mock call EXPECT()
		ch <- &redis.Message{Channel: "__keyevent@1__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("empty output", func(t *testing.T) {
		key := "foo:admins:PR:4:d91f6ff"

		testRedis.EXPECT().Run(gomock.Any(), key).
			Return(&tfaplv1beta1.Run{Module: types.NamespacedName{Namespace: "foo", Name: "admins"}, Request: &tfaplv1beta1.Request{PR: &tfaplv1beta1.PullRequest{Number: 4, CommentID: 123}}, CommitHash: "hash1", Output: ""}, nil)

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})
}

func Test_findPRNumber(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want int
	}{
		{"1", `Merge pull request #53 from utilitywarehouse/as-api`, 53},
		{"2", `Merge pull request #123304040 from utilitywarehouse/as-api`, 123304040},
		{"3", `remove duplicate groups`, 0},
		{"5", `Something for customer data (#13531)`, 13531},
		{"6", `whatever tis this . (#13529)`, 13529},
		{"7", `(#13529)`, 13529},
		{"8", `some (#13529) else `, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findPRNumber(tt.msg); got != tt.want {
				t.Errorf("findPRNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}
