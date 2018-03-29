package cluster

import (
	"context"
	"fmt"

	"github.com/fabric8-services/fabric8-wit/log"
	errs "github.com/pkg/errors"
)

// Resolve a func to resolve a cluster
type Resolve func(ctx context.Context, target string) (Cluster, error)

// NewResolve returns a new Cluster
func NewResolve(clusterService Service) Resolve {
	return func(ctx context.Context, target string) (Cluster, error) {
		clusters, err := clusterService.GetClusters(context.Background())
		if err != nil {
			log.Panic(nil, map[string]interface{}{
				"err": err,
			}, "unable to resolve clusters")
			return Cluster{}, errs.Wrapf(err, "unable to resolve cluster")
		}
		for _, cluster := range clusters {
			log.Debug(nil, map[string]interface{}{"target_url": cleanURL(target), "cluster_url": cleanURL(cluster.APIURL)}, "comparing URLs...")
			if cleanURL(target) == cleanURL(cluster.APIURL) {
				return cluster, nil
			}
		}
		return Cluster{}, fmt.Errorf("unable to resolve cluster")
	}
}
