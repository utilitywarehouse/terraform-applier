package runner

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"sync"

	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

var pluginCacheMain = "plugin-cache-main"

// The plugin cache directory is not guaranteed to be concurrency safe.
// https://github.com/hashicorp/terraform/issues/31964

// pluginCache provides cache dir copy on request.
// it keeps main copy of the all cached providers called `main`.
// when new cache dir is requested main dir will be cloned.
// once worker is done with cache dir any newly installed providers will be
// copied back to main cache dir for re-use in next run.
type pluginCache struct {
	*sync.RWMutex
	log  *slog.Logger
	main string
	tmp  string
}

func newPluginCache(log *slog.Logger, root, tmp string) (*pluginCache, error) {
	main := path.Join(root, pluginCacheMain)

	if err := os.MkdirAll(main, defaultDirMode); err != nil {
		return nil, fmt.Errorf("unable to create main cache dir err:%w", err)
	}

	return &pluginCache{
		&sync.RWMutex{},
		log,
		main,
		tmp,
	}, nil
}

// new returns path of the plugin cache dir which is the clone
// of the main plugin cache dir
func (pcp *pluginCache) new() string {

	tmpPC, err := os.MkdirTemp(pcp.tmp, "plugin-cache-*")
	if err != nil {
		pcp.log.Error("unable to create plugin cache dir", "err", err)
		return ""
	}

	pcp.RLock()
	defer pcp.RUnlock()

	if err := sysutil.CopyDir(pcp.main, tmpPC, true); err != nil {
		pcp.log.Error("unable to create copy providers to plugin cache dir", "err", err)
		return ""
	}

	return tmpPC
}

// done will copy newly downloaded provides from given plugin dir to main
// plugin dir so it can be reused.
func (pcp *pluginCache) done(tmpPC string) {
	defer sysutil.RemoveAll(tmpPC)

	pcp.Lock()
	defer pcp.Unlock()

	if err := sysutil.CopyDir(tmpPC, pcp.main, false); err != nil {
		pcp.log.Error("unable to create copy tmp to main dir", "err", err)
	}
}
