modified:   go.mod
modified:   go.sum
modified:   pkg/controller/mongodb/helpers.go
modified:   pkg/controller/mongodb/horizontal_scaling.go
modified:   pkg/controller/mongodb/horizontal_scaling_shard.go
modified:   pkg/controller/mongodb/reconfigure.go
modified:   pkg/controller/mongodb/reconfigure_tls.go
modified:   pkg/controller/mongodb/restart.go/*
Copyright AppsCode Inc. and Contributors

Licensed under the AppsCode Free Trial License 1.0.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/appscode/licenses/raw/1.0.0/AppsCode-Free-Trial-1.0.0.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mongodb

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha2"
	dbaapi "kubedb.dev/apimachinery/apis/ops/v1alpha1"
	dbutil "kubedb.dev/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha2/util"
	dbautil "kubedb.dev/apimachinery/client/clientset/versioned/typed/ops/v1alpha1/util"
	"kubedb.dev/db-client-go/mongodb"
	"kubedb.dev/ops-manager/pkg/controller/lib"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kmapi "kmodules.xyz/client-go/api/v1"
)

// VerticalScale sets opsReq condition from Pending to Progressing, pause the db, & then calls corresponding functions based on the db-type
func (c *mongoOpsReqController) VerticalScale() error {
	var err error
	log := c.log
	if c.req.Status.Phase == dbaapi.OpsRequestPhasePending {
		newOpsReq, err := dbautil.UpdateMongoDBOpsRequestStatus(
			context.TODO(),
			c.DBClient.OpsV1alpha1(),
			c.req.ObjectMeta,
			func(status *dbaapi.OpsRequestStatus) (types.UID, *dbaapi.OpsRequestStatus) {
				status.Phase = dbaapi.Progressing
				status.ObservedGeneration = c.req.Generation
				status.Conditions = kmapi.SetCondition(status.Conditions, kmapi.NewCondition(dbaapi.VerticalScalingDatabase, "MongoDB ops request is vertically scaling database", c.req.Generation))
				return c.req.UID, status
			}, metav1.UpdateOptions{})
		if err != nil {
			log.Error(err, "failed to update status")
			return err
		}
		c.req.Status = newOpsReq.Status
	}

	scaledAll := false

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, dbaapi.UpdateStatefulSetResources) {
		err := c.pauseMongoDB()
		if err != nil {
			log.Error(err, "failed to pause mongoDB")
			return err
		}
	}

	if c.db.Spec.ShardTopology == nil {
		if c.req.Spec.VerticalScaling.Standalone != nil {
			err, scaledAll = c.vsStandalone()
			if err != nil {
				return err
			}
		} else if c.req.Spec.VerticalScaling.ReplicaSet != nil {
			err, scaledAll = c.vsReplicaset()
			if err != nil {
				return err
			}
		}
	} else {
		err, scaledAll = c.vsShardedCluster()
		if err != nil {
			return err
		}
	}

	if scaledAll {
		err := c.vsSucceeded()
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *mongoOpsReqController) vsStandalone() (error, bool) {
	condition := dbaapi.UpdateStandaloneResources
	standalone := c.req.Spec.VerticalScaling.Standalone

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, dbaapi.UpdateStatefulSetResources) {
		err := c.updateStatefulSetsResources(c.db.OffshootName(), standalone, c.req.Spec.VerticalScaling.Exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.OffshootName())
			return err, false
		}

		if c.req.Spec.Configuration != nil {
			err = c.updateStsForReconfig(c.req.Spec.Configuration.Standalone, c.db.Spec.ConfigSecret, "", []string{c.db.OffshootName()})
			if err != nil {
				return err, false
			}
		}

		err = c.UpdateMongoOpsReqConditions(dbaapi.UpdateStatefulSetResources, "Successfully updated StatefulSets Resources")
		if err != nil {
			c.log.Error(err, "failed to update conditions")
			return err, false
		}
		c.Recorder.Event(
			c.req,
			core.EventTypeNormal,
			dbaapi.UpdateStatefulSetResources,
			"Successfully updated StatefulSets Resources",
		)
		c.log.Info("Successfully updated StatefulSets Resources", "StatefulSet", c.db.OffshootName())
	}

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, condition) {
		c.RunParallel(condition,
			"Successfully Vertically Scaled Standalone Resources",
			c.newRestartFunc(
				[][]string{c.podNames()},
				isPodResourceUpdated,
				isPodResourceUpdated,
				[][]string{}))
		return nil, false
	}

	return nil, kmapi.IsConditionTrue(c.req.Status.Conditions, condition)
}

// flow to vsStandalone, vsReplicaset & vsShardedCluster is exactly the same.
// At first, they directly patch the statefulSet Resources
// update opsReq condition accordingly
// if user asked for reconfiguration, call updateStsForReconfig() from reconfigure.go
// Al last, call restartFunc with masterFunc & replicaFunc to be isPodResourceUpdated
//
func (c *mongoOpsReqController) vsReplicaset() (error, bool) {
	condition := dbaapi.UpdateReplicaSetResources
	replicaset := c.req.Spec.VerticalScaling.ReplicaSet
	arbiter := c.req.Spec.VerticalScaling.Arbiter
	hidden := c.req.Spec.VerticalScaling.Hidden

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, dbaapi.UpdateStatefulSetResources) {
		err := c.updateStatefulSetsResources(c.db.OffshootName(), replicaset, c.req.Spec.VerticalScaling.Exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.OffshootName())
			return err, false
		}

		err = c.updateStatefulSetsResources(c.db.ArbiterNodeName(), arbiter, c.req.Spec.VerticalScaling.Exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.ArbiterNodeName())
			return err, false
		}

		err = c.updateStatefulSetsResources(c.db.HiddenNodeName(), hidden, c.req.Spec.VerticalScaling.Exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.HiddenNodeName())
			return err, false
		}

		if c.req.Spec.Configuration != nil {
			err = c.updateStsForReconfig(c.req.Spec.Configuration.ReplicaSet, c.db.Spec.ConfigSecret, "", []string{c.db.OffshootName()})
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.Arbiter, c.db.Spec.Arbiter.ConfigSecret, api.NodeTypeArbiter, []string{c.db.ArbiterNodeName()})
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.Hidden, c.db.Spec.Hidden.ConfigSecret, api.NodeTypeHidden, []string{c.db.HiddenNodeName()})
			if err != nil {
				return err, false
			}

			c.log.Info("Reconfiguration Succeeded")
		}

		err = c.UpdateMongoOpsReqConditions(dbaapi.UpdateStatefulSetResources, "Successfully updated StatefulSets Resources")
		if err != nil {
			c.log.Error(err, "failed to update conditions")
			return err, false
		}
		c.Recorder.Event(
			c.req,
			core.EventTypeNormal,
			dbaapi.UpdateStatefulSetResources,
			"Successfully updated StatefulSets Resources",
		)
		c.log.Info("Successfully updated StatefulSets Resources", "StatefulSet", c.nodeNames())
	}

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, condition) {
		c.RunParallel(condition,
			"Successfully Vertically Scaled Replicaset Resources",
			c.newRestartFunc(
				[][]string{c.podNames()},
				isPodResourceUpdated,
				isPodResourceUpdated,
				[][]string{}))
		return nil, false
	}

	return nil, kmapi.IsConditionTrue(c.req.Status.Conditions, condition)
}

func (c *mongoOpsReqController) vsShardedCluster() (error, bool) {
	mongos := c.req.Spec.VerticalScaling.Mongos
	configServer := c.req.Spec.VerticalScaling.ConfigServer
	shard := c.req.Spec.VerticalScaling.Shard
	exporter := c.req.Spec.VerticalScaling.Exporter
	arbiter := c.req.Spec.VerticalScaling.Arbiter
	hidden := c.req.Spec.VerticalScaling.Hidden

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, dbaapi.UpdateStatefulSetResources) {
		// mongos
		err := c.updateStatefulSetsResources(c.db.MongosNodeName(), mongos, exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.MongosNodeName())
			return err, false
		}
		c.log.Info("Successfully updated StatefulSet Resources", "StatefulSet", c.db.MongosNodeName())

		// configServer
		err = c.updateStatefulSetsResources(c.db.ConfigSvrNodeName(), configServer, exporter)
		if err != nil {
			c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.ConfigSvrNodeName())
			return err, false
		}
		c.log.Info("Successfully updated StatefulSet Resources", "StatefulSet", c.db.ConfigSvrNodeName())

		// shard & arbiter
		statefulSetNames := make([]string, c.db.Spec.ShardTopology.Shard.Shards)
		arbStsNames := make([]string, c.db.Spec.ShardTopology.Shard.Shards)
		hiddenStsNames := make([]string, c.db.Spec.ShardTopology.Shard.Shards)

		for i := int32(0); i < c.db.Spec.ShardTopology.Shard.Shards; i++ {
			statefulSetNames[i] = c.db.ShardNodeName(i)
			err := c.updateStatefulSetsResources(c.db.ShardNodeName(i), shard, exporter)
			if err != nil {
				c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.ShardNodeName(i))
				return err, false
			}
			c.log.Info("Successfully updated StatefulSets Resources", "StatefulSet", c.db.ShardNodeName(i))

			arbStsNames[i] = c.db.ArbiterShardNodeName(i)
			err = c.updateStatefulSetsResources(c.db.ArbiterShardNodeName(i), arbiter, exporter)
			if err != nil {
				c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.ArbiterShardNodeName(i))
				return err, false
			}
			c.log.Info("Successfully updated StatefulSets Resources", "StatefulSet", c.db.ArbiterShardNodeName(i))

			hiddenStsNames[i] = c.db.HiddenShardNodeName(i)
			err = c.updateStatefulSetsResources(c.db.HiddenShardNodeName(i), hidden, exporter)
			if err != nil {
				c.log.Error(err, "failed to update statefulSetResources", "StatefulSet", c.db.HiddenShardNodeName(i))
				return err, false
			}
			c.log.Info("Successfully updated StatefulSets Resources", "StatefulSet", c.db.HiddenShardNodeName(i))
		}

		if c.req.Spec.Configuration != nil {
			err = c.updateStsForReconfig(c.req.Spec.Configuration.Mongos, c.db.Spec.ShardTopology.Mongos.ConfigSecret, api.NodeTypeMongos, []string{c.db.MongosNodeName()})
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.ConfigServer, c.db.Spec.ShardTopology.ConfigServer.ConfigSecret, api.NodeTypeConfig, []string{c.db.ConfigSvrNodeName()})
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.Shard, c.db.Spec.ShardTopology.Shard.ConfigSecret, api.NodeTypeShard, statefulSetNames)
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.Arbiter, c.db.Spec.Arbiter.ConfigSecret, api.NodeTypeArbiter, arbStsNames)
			if err != nil {
				return err, false
			}

			err = c.updateStsForReconfig(c.req.Spec.Configuration.Hidden, c.db.Spec.Hidden.ConfigSecret, api.NodeTypeHidden, hiddenStsNames)
			if err != nil {
				return err, false
			}
		}

		c.Recorder.Event(
			c.req,
			core.EventTypeNormal,
			dbaapi.UpdateStatefulSetResources,
			"Successfully updated StatefulSets Resources",
		)
		err = c.UpdateMongoOpsReqConditions(dbaapi.UpdateStatefulSetResources, "Successfully updated StatefulSets Resources")
		if err != nil {
			c.log.Error(err, "failed to update conditions")
			return err, false
		}
		c.log.Info("Successfully updated StatefulSets Resources")
	}

	configServerUpdated := configServer == nil
	if configServer != nil {
		condition := dbaapi.UpdateConfigServerResources
		if !kmapi.IsConditionTrue(c.req.Status.Conditions, condition) {
			c.RunParallel(condition,
				"Successfully Vertically Scaled ConfigServer Resources",
				c.newRestartFunc(
					[][]string{c.configServerPodNames()},
					isPodResourceUpdated,
					isPodResourceUpdated,
					[][]string{}))
			return nil, false
		}
		configServerUpdated = kmapi.IsConditionTrue(c.req.Status.Conditions, condition)
	}

	shardUpdated := shard == nil
	if shard != nil {
		condition := dbaapi.UpdateShardResources
		if !kmapi.IsConditionTrue(c.req.Status.Conditions, condition) {
			c.RunParallel(condition,
				"Successfully Vertically Scaled Shard Resources",
				c.newRestartFunc(c.shardPodNames(),
					isPodResourceUpdated,
					isPodResourceUpdated,
					[][]string{}))
			return nil, false
		}
		shardUpdated = kmapi.IsConditionTrue(c.req.Status.Conditions, condition)
	}

	mongosUpdated := mongos == nil
	if mongos != nil {
		condition := dbaapi.UpdateMongosResources
		if !kmapi.IsConditionTrue(c.req.Status.Conditions, condition) {
			c.RunParallel(condition,
				"Successfully Vertically Scaled Mongos Resources",
				c.newRestartFunc(
					[][]string{c.mongosPodNames()},
					isPodResourceUpdated,
					isPodResourceUpdated,
					[][]string{}))
			return nil, false
		}
		mongosUpdated = kmapi.IsConditionTrue(c.req.Status.Conditions, condition)
	}

	return nil, configServerUpdated && shardUpdated && mongosUpdated
}

// patch mongoDB with the updated resources, resume db, resume backupConfiguration, update opsReq phase tobe successful
func (c *mongoOpsReqController) vsSucceeded() error {
	standalone := c.req.Spec.VerticalScaling.Standalone
	replicaset := c.req.Spec.VerticalScaling.ReplicaSet
	mongos := c.req.Spec.VerticalScaling.Mongos
	configServer := c.req.Spec.VerticalScaling.ConfigServer
	shard := c.req.Spec.VerticalScaling.Shard
	exporter := c.req.Spec.VerticalScaling.Exporter
	arbiter := c.req.Spec.VerticalScaling.Arbiter

	var err error
	c.db, _, err = dbutil.CreateOrPatchMongoDB(context.TODO(), c.DBClient.KubedbV1alpha2(), c.db.ObjectMeta, func(db *api.MongoDB) *api.MongoDB {
		db = c.getUpdatedDB(db)
		if db.Spec.ShardTopology != nil {
			if shard != nil {
				db.Spec.ShardTopology.Shard.PodTemplate.Spec.Resources = *shard
			}
			if mongos != nil {
				db.Spec.ShardTopology.Mongos.PodTemplate.Spec.Resources = *mongos
			}
			if configServer != nil {
				db.Spec.ShardTopology.ConfigServer.PodTemplate.Spec.Resources = *configServer
			}
		} else {
			if standalone != nil {
				db.Spec.PodTemplate.Spec.Resources = *standalone
			}
			if replicaset != nil {
				db.Spec.PodTemplate.Spec.Resources = *replicaset
			}
		}

		if db.Spec.Arbiter != nil && arbiter != nil {
			db.Spec.Arbiter.PodTemplate.Spec.Resources = *arbiter
		}

		if db.Spec.Monitor != nil && exporter != nil {
			db.Spec.Monitor.Prometheus.Exporter.Resources = *exporter
		}

		return db
	}, metav1.PatchOptions{})
	if err != nil {
		c.log.Error(err, "failed to patch mongoDB")
		return err
	}

	err = c.resumeMongoDB()
	if err != nil {
		c.log.Error(err, "failed to resume mongoDB")
		return err
	}

	conditions, err := lib.ResumeBackupConfiguration(c.Initializers.Stash.StashClient.StashV1beta1(), c.db.ObjectMeta, c.req.Status.Conditions, c.req.Generation)
	if err != nil {
		return err
	}
	if conditions != nil {
		newOpsReq, err := dbautil.UpdateMongoDBOpsRequestStatus(
			context.TODO(),
			c.DBClient.OpsV1alpha1(),
			c.req.ObjectMeta,
			func(in *dbaapi.OpsRequestStatus) (types.UID, *dbaapi.OpsRequestStatus) {
				in.Conditions = conditions
				return c.req.UID, in
			}, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		c.req.Status = newOpsReq.Status
	}

	if !kmapi.IsConditionTrue(c.req.Status.Conditions, dbaapi.Successful) {
		c.Recorder.Event(
			c.req,
			core.EventTypeNormal,
			dbaapi.Successful,
			"Successfully Vertically Scaled Database",
		)
		c.log.V(2).Info("Successfully Vertically Scaled Database")
		newOpsReq, err := dbautil.UpdateMongoDBOpsRequestStatus(
			context.TODO(),
			c.DBClient.OpsV1alpha1(),
			c.req.ObjectMeta,
			func(status *dbaapi.OpsRequestStatus) (types.UID, *dbaapi.OpsRequestStatus) {
				status.Phase = dbaapi.OpsRequestPhaseSuccessful
				status.ObservedGeneration = c.req.Generation
				status.Conditions = kmapi.SetCondition(status.Conditions, kmapi.NewCondition(dbaapi.Successful, "Successfully Vertically Scaled Database", c.req.Generation))
				return c.req.UID, status
			}, metav1.UpdateOptions{})
		if err != nil {
			c.log.Error(err, "failed to update status")
			return err
		}
		c.req.Status = newOpsReq.Status
	}
	return nil
}

func isPodResourceUpdated(opts *mongoOpsReqController, podName string, podNames []string) error {
	idx := strings.LastIndex(podName, "-")
	stsName := podName[:idx]

	checkMongoDBContainer := false
	if opts.db.Spec.ShardTopology != nil {
		if (strings.Contains(stsName, opts.db.MongosNodeName()) && opts.req.Spec.VerticalScaling.Mongos != nil) ||
			(strings.Contains(stsName, opts.db.ShardCommonNodeName()) && opts.req.Spec.VerticalScaling.Shard != nil) ||
			(strings.Contains(stsName, opts.db.ShardCommonNodeName()) && strings.HasSuffix(stsName, api.NodeTypeArbiter) && opts.req.Spec.VerticalScaling.Arbiter != nil) ||
			(strings.Contains(stsName, opts.db.ConfigSvrRepSetName()) && opts.req.Spec.VerticalScaling.ConfigServer != nil) {
			checkMongoDBContainer = true
		}
	} else if opts.db.Spec.ShardTopology == nil && opts.db.Spec.ReplicaSet != nil &&
		(opts.req.Spec.VerticalScaling.ReplicaSet != nil || opts.req.Spec.VerticalScaling.Arbiter != nil) {
		checkMongoDBContainer = true
	} else if opts.db.Spec.ShardTopology == nil && opts.db.Spec.ReplicaSet == nil && opts.req.Spec.VerticalScaling.Standalone != nil {
		checkMongoDBContainer = true
	}

	sts, err := opts.Client.AppsV1().StatefulSets(opts.db.Namespace).Get(context.TODO(), stsName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	pod, err := opts.Client.CoreV1().Pods(opts.req.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i, podContainer := range pod.Spec.Containers {
		for j, stsContainer := range sts.Spec.Template.Spec.Containers {
			if podContainer.Name == stsContainer.Name {
				if (opts.req.Spec.VerticalScaling.Exporter != nil && stsContainer.Name == api.ContainerExporterName) || (checkMongoDBContainer && stsContainer.Name == api.MongoDBContainerName) {
					if !reflect.DeepEqual(pod.Spec.Containers[i].Resources, sts.Spec.Template.Spec.Containers[j].Resources) {
						return fmt.Errorf("MongoDBOpsRequest %s/%s: resources have not updated yet", opts.req.Namespace, opts.req.Name)
					}
				}
			}
		}
	}

	mongoClient, err := mongodb.NewKubeDBClientBuilder(opts.KBClient, opts.db).
		WithPod(podName).
		WithCerts(opts.certs).
		GetMongoClient()
	if err != nil {
		return err
	}

	mongoClient.Close()

	return nil
}

modified:   pkg/controller/mongodb/upgrade.go
modified:   pkg/controller/mongodb/util.go
deleted:    pkg/controller/mongodb/vertical_scaling.go
modified:   vendor/kubedb.dev/apimachinery/apis/kubedb/v1alpha2/constants.go
modified:   vendor/kubedb.dev/apimachinery/apis/ops/v1alpha1/type.go
modified:   vendor/kubedb.dev/mysql/pkg/admission/validator.go
modified:   vendor/kubedb.dev/mysql/pkg/controller/statefulset.go
modified:   vendor/modules.txt