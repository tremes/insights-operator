package configobserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openshift/insights-operator/pkg/config"
)

type Interface interface {
	Config() *config.InsightsConfiguration
	ConfigChanged() (<-chan struct{}, func())
	Listen(ctx context.Context)
}

// ConfigAggregator is an auxiliary structure that should obviate the need for the use of
// legacy secret configurator and the new config map informer
type ConfigAggregator struct {
	lock               sync.Mutex
	legacyConfigurator Configurator
	configMapInformer  ConfigMapInformer
	configAggregated   *config.InsightsConfiguration
	listeners          map[chan struct{}]struct{}
}

func NewConfigAggregator(ctrl Configurator, configMapInf ConfigMapInformer) Interface {
	confAggreg := &ConfigAggregator{
		legacyConfigurator: ctrl,
		configMapInformer:  configMapInf,
		listeners:          make(map[chan struct{}]struct{}),
	}
	confAggreg.aggregate()
	return confAggreg
}

func (c *ConfigAggregator) aggregate() {
	legacyConfig := c.legacyConfigurator.Config()
	newConf := c.configMapInformer.Config()
	conf := &config.InsightsConfiguration{
		DataReporting: config.DataReporting{
			Interval:         legacyConfig.Interval,
			UploadEndpoint:   legacyConfig.Endpoint,
			DownloadEndpoint: legacyConfig.ReportEndpoint,
			// TODO ??? this is based on the token from pull-secret
			Enabled: legacyConfig.Report,
		},
	}

	if c.configMapInformer.ConfigMap() == nil {
		c.configAggregated = conf
		return
	}

	// read config map values and merge
	if newConf.DataReporting.Interval != 0*time.Minute {
		conf.DataReporting.Interval = newConf.DataReporting.Interval
	}

	if newConf.DataReporting.UploadEndpoint != "" {
		conf.DataReporting.UploadEndpoint = newConf.DataReporting.UploadEndpoint
	}

	c.configAggregated = conf
	fmt.Println("========================= CONFIG MERGED", c.configAggregated)

}

func (c *ConfigAggregator) Config() *config.InsightsConfiguration {
	c.aggregate()
	return c.configAggregated
}

// Listen listens to the legacy Secret configurator/observer as well as the
// new config map informer. When any configuration change is observed then all the listeners
// are notified.
func (c *ConfigAggregator) Listen(ctx context.Context) {
	legacyCh, legacyCloseFn := c.legacyConfigurator.ConfigChanged()
	cmCh, cmICloseFn := c.configMapInformer.ConfigChanged()
	defer func() {
		legacyCloseFn()
		cmICloseFn()
	}()

	for {
		select {
		case <-legacyCh:
			fmt.Println("==================================== LEGACY NOTIFIED  ", len(legacyCh), cap(legacyCh))
			for ch := range c.listeners {
				fmt.Println("================= LEGACY SENDING ", ch, len(ch), cap(ch))
				ch <- struct{}{}
			}
		case <-cmCh:
			fmt.Println("==================================== CM NOTIFIED ", len(cmCh), cap(cmCh))
			for ch := range c.listeners {
				fmt.Println("================= NEW SENDING ", ch, len(ch), cap(ch))
				ch <- struct{}{}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *ConfigAggregator) ConfigChanged() (<-chan struct{}, func()) {
	c.lock.Lock()
	defer c.lock.Unlock()
	ch := make(chan struct{}, 1)
	c.listeners[ch] = struct{}{}
	fmt.Println("=================== LISTENERS LENGTH ", len(c.listeners), ch)
	return ch, func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		close(ch)
		fmt.Println("========================= DELETING ", ch)
		delete(c.listeners, ch)
	}
}
