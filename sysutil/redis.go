package sysutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	PRKeyExpirationDur = 7 * 24 * time.Hour
	ErrKeyNotFound     = errors.New("key not found")
)

//go:generate go run github.com/golang/mock/mockgen -package sysutil -destination redis_mock.go github.com/utilitywarehouse/terraform-applier/sysutil RedisInterface

// RedisInterface allows for mocking out the functionality of DB when testing
type RedisInterface interface {
	DefaultLastRun(ctx context.Context, module types.NamespacedName) (*tfaplv1beta1.Run, error)
	DefaultApply(ctx context.Context, module types.NamespacedName) (*tfaplv1beta1.Run, error)
	PRRun(ctx context.Context, module types.NamespacedName, pr int, hash string) (*tfaplv1beta1.Run, error)
	Run(ctx context.Context, key string) (*tfaplv1beta1.Run, error)
	Runs(ctx context.Context, module types.NamespacedName, keySuffix string) ([]*tfaplv1beta1.Run, error)
	GetCommitHash(ctx context.Context, key string) (string, error)

	SetDefaultLastRun(ctx context.Context, run *tfaplv1beta1.Run) error
	SetDefaultApply(ctx context.Context, run *tfaplv1beta1.Run) error
	SetPRRun(ctx context.Context, run *tfaplv1beta1.Run) error
}

type Redis struct {
	Client *redis.Client
}

func keyPrefix(module types.NamespacedName) string {
	return fmt.Sprintf("%s:%s:", module.Namespace, module.Name)
}

func defaultLastRunKey(module types.NamespacedName) string {
	return fmt.Sprintf("%sdefault:lastRun", keyPrefix(module))
}

func defaultLastApplyKey(module types.NamespacedName) string {
	return fmt.Sprintf("%sdefault:lastApply", keyPrefix(module))
}

func DefaultPRLastRunsKey(module types.NamespacedName, pr int, hash string) string {
	return fmt.Sprintf("%sPR:%d:%s", keyPrefix(module), pr, hash)
}

// DefaultLastRun will return last run result for the default branch
func (r Redis) DefaultLastRun(ctx context.Context, module types.NamespacedName) (*tfaplv1beta1.Run, error) {
	return r.Run(ctx, defaultLastRunKey(module))
}

// DefaultApply will return last apply run's result for the default branch
func (r Redis) DefaultApply(ctx context.Context, module types.NamespacedName) (*tfaplv1beta1.Run, error) {
	return r.Run(ctx, defaultLastApplyKey(module))
}

// PRLastRun will return last run result for the given PR branch
func (r Redis) PRRun(ctx context.Context, module types.NamespacedName, pr int, hash string) (*tfaplv1beta1.Run, error) {
	return r.Run(ctx, DefaultPRLastRunsKey(module, pr, hash))
}

// Runs will return all the runs stored for the given module
func (r Redis) Runs(ctx context.Context, module types.NamespacedName, patternSuffix string) ([]*tfaplv1beta1.Run, error) {
	var runs []*tfaplv1beta1.Run

	keys, err := r.Client.Keys(ctx, keyPrefix(module)+patternSuffix).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("unable to get module keys err:%w", err)
	}

	for _, key := range keys {
		run, err := r.Run(ctx, key)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// SetDefaultLastRun puts given run in to cache with no expiration
func (r Redis) SetDefaultLastRun(ctx context.Context, run *tfaplv1beta1.Run) error {
	return r.setKV(ctx, defaultLastRunKey(run.Module), run, 0)
}

// SetDefaultApply puts given run in to cache with no expiration
func (r Redis) SetDefaultApply(ctx context.Context, run *tfaplv1beta1.Run) error {
	return r.setKV(ctx, defaultLastApplyKey(run.Module), run, 0)
}

// SetPRRun puts given run in to cache with expiration
func (r Redis) SetPRRun(ctx context.Context, run *tfaplv1beta1.Run) error {
	return r.setKV(ctx, DefaultPRLastRunsKey(run.Module, run.Request.PR.Number, run.CommitHash), run, PRKeyExpirationDur)
}

func (r Redis) setKV(ctx context.Context, key string, run *tfaplv1beta1.Run, exp time.Duration) error {
	str, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("unable to marshal run err:%w", err)
	}

	return r.Client.Set(ctx, key, str, exp).Err()
}

func (r Redis) GetCommitHash(ctx context.Context, key string) (string, error) {
	module, err := r.Run(ctx, key)
	if err != nil {
		return "", fmt.Errorf("unable to get key value pair err:%w", err)
	}

	return module.CommitHash, nil
}

func (r Redis) Run(ctx context.Context, key string) (*tfaplv1beta1.Run, error) {
	output, err := r.Client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, ErrKeyNotFound
	} else if err != nil {
		return nil, fmt.Errorf("unable to get value err:%w", err)
	}

	run := tfaplv1beta1.Run{}
	if err := json.Unmarshal([]byte(output), &run); err != nil {
		return nil, fmt.Errorf("unable to unmarshal run err:%w", err)
	}

	return &run, nil
}
