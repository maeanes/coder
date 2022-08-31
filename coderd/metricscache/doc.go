// Package metricscache holds the DAU cache and, eventually, the
// user activity cache. The aggregation queries responsible for these values
// can take up to a minute on large deployments, but the cache has near zero
// effect on most deploymentsd.
package metricscache
