package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/redis/go-redis/v9"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
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

func Test_checkPRCommentForOutputRequests(t *testing.T) {
	ctx := context.Background()
	goMockCtrl := gomock.NewController(t)
	testRedis := sysutil.NewMockRedisInterface(goMockCtrl)

	planner := &Planner{
		Log:         slog.Default(),
		RedisClient: testRedis,
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	mockRuns := []*v1beta1.Run{
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:01:05Z")}, CommitHash: "hash1", Output: "terraform plan output"},
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:02:05Z")}},
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:04:05Z")}, CommitHash: "hash1", Module: types.NamespacedName{Namespace: "foo", Name: "bar"}, Summary: "plan summary", Output: "terraform plan output"},
	}

	testRedis.EXPECT().Runs(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "two"}).
		Return(mockRuns, nil).AnyTimes()

	t.Run("terraform plan output comment", func(t *testing.T) {
		comment := prComment{
			Body: runOutputMsg("foo/two", "foo/two", &v1beta1.Run{CommitHash: "hash1", Summary: "Plan: x to add, x to change, x to destroy.", Output: "terraform plan output"}),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty request time", func(t *testing.T) {
		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedMsgTml, "foo/two", ""),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("run not found in redis", func(t *testing.T) {
		comment := prComment{
			Body: requestAcknowledgedMsg("foo/two", "foo/two", mustParseMetaTime("2023-04-02T15:03:05Z")),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty run output in redis", func(t *testing.T) {
		comment := prComment{
			Body: requestAcknowledgedMsg("foo/two", "foo/two", mustParseMetaTime("2023-04-02T15:02:05Z")),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan output ready in redis", func(t *testing.T) {
		comment := prComment{
			Body:       requestAcknowledgedMsg("foo/two", "module/path/is/going/to/be/here", mustParseMetaTime("2023-04-02T15:04:05Z")),
			DatabaseID: 111,
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{
			Body: runOutputMsg("foo/two", "module/path/is/going/to/be/here", &v1beta1.Run{CommitHash: "hash1", Summary: "plan summary", Output: "terraform plan output"}),
		}
		wantOk := true

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_processRedisKeySetMsg(t *testing.T) {
	ctx := context.Background()
	err := v1beta1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	kubeClient := fake.NewFakeClient(
		&v1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "admins", Namespace: "foo"},
			Spec: v1beta1.ModuleSpec{
				RepoURL: "https://github.com/utilitywarehouse/terraform-applier.git",
				Path:    "foo/admins",
			},
		},
		&v1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "users", Namespace: "foo",
			},
			Spec: v1beta1.ModuleSpec{
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

		testRedis.EXPECT().ParsePRRunsKey(key).
			Return(types.NamespacedName{Namespace: "foo", Name: "admins"}, 4, "d91f6ff", nil)

		testRedis.EXPECT().PRRun(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "admins"}, 4, "d91f6ff").
			Return(&v1beta1.Run{Request: &v1beta1.Request{PR: &v1beta1.PullRequest{CommentID: 123}}, CommitHash: "hash1", Output: "terraform plan output"}, nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), 123, 4, gomock.Any()).
			DoAndReturn(func(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
				if repo.Path != "utilitywarehouse" &&
					repo.Repo != "terraform-applier" {
					t.Fatalf("repo name is not matching: %s", repo)
				}
				return 123, nil
			})

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("valid PR key updated module2", func(t *testing.T) {
		key := "foo:users:PR:4:d91f6ff"

		testRedis.EXPECT().ParsePRRunsKey(key).
			Return(types.NamespacedName{Namespace: "foo", Name: "users"}, 4, "d91f6ff", nil)

		testRedis.EXPECT().PRRun(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "users"}, 4, "d91f6ff").
			Return(&v1beta1.Run{Request: &v1beta1.Request{PR: &v1beta1.PullRequest{CommentID: 123}}, CommitHash: "hash1", Output: "terraform plan output"}, nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), 123, 4, gomock.Any()).
			DoAndReturn(func(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
				if repo.Path != "utilitywarehouse" &&
					repo.Repo != "terraform-applier" {
					t.Fatalf("repo name is not matching: %s", repo)
				}
				return 123, nil
			})

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

	t.Run("invalid key", func(t *testing.T) {
		key := "foo:admins:lastRun"
		// not expecting any other calls
		// hence no mock call EXPECT()
		ch <- &redis.Message{Channel: "__keyevent@1__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})

	t.Run("empty output", func(t *testing.T) {
		key := "foo:admins:PR:4:d91f6ff"

		testRedis.EXPECT().ParsePRRunsKey(key).
			Return(types.NamespacedName{Namespace: "foo", Name: "admins"}, 4, "d91f6ff", nil)

		testRedis.EXPECT().PRRun(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "admins"}, 4, "d91f6ff").
			Return(&v1beta1.Run{Request: &v1beta1.Request{PR: &v1beta1.PullRequest{CommentID: 123}}, CommitHash: "hash1", Output: ""}, nil)

		ch <- &redis.Message{Channel: "__keyevent@0__:set", Payload: key}
		time.Sleep(2 * time.Second)
	})
}
